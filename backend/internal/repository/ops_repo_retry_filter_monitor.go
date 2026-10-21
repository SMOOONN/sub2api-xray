package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const retryFilterEventsCTE = `
WITH retry_events AS (
  SELECT
    e.id AS error_id,
    CASE WHEN COALESCE(ev.value->>'at_unix_ms', '') ~ '^[0-9]{1,15}$'
         THEN CASE WHEN (ev.value->>'at_unix_ms')::bigint BETWEEN 1 AND 253402300799999
                   THEN to_timestamp((ev.value->>'at_unix_ms')::bigint / 1000)
                        + ((ev.value->>'at_unix_ms')::bigint % 1000) * INTERVAL '1 millisecond'
                   ELSE e.created_at END
         ELSE e.created_at END AS created_at,
    COALESCE(e.request_id, '') AS request_id,
    CASE WHEN COALESCE(ev.value->>'account_id', '') ~ '^[1-9][0-9]*$'
         THEN (ev.value->>'account_id')::bigint ELSE e.account_id END AS account_id,
    e.group_id,
    COALESCE(NULLIF(e.requested_model, ''), e.model, '') AS model,
    ev.ordinality,
    CASE WHEN COALESCE(ev.value->>'retry_filter_preset_id', '') ~ '^[0-9]+$'
         THEN (ev.value->>'retry_filter_preset_id')::bigint ELSE 0 END AS preset_id,
    CASE WHEN COALESCE(ev.value->>'retry_filter_rule_id', '') ~ '^[0-9]+$'
         THEN (ev.value->>'retry_filter_rule_id')::bigint ELSE 0 END AS rule_id,
    COALESCE(ev.value->>'retry_filter_action', '') AS action,
    CASE WHEN COALESCE(ev.value->>'upstream_status_code', '') ~ '^-?[0-9]+$'
         THEN (ev.value->>'upstream_status_code')::integer ELSE 0 END AS status_code,
    COALESCE(ev.value->>'reason', '') AS reason,
    COALESCE(NULLIF(ev.value->>'detail', ''), ev.value->>'message', '') AS detail,
    COALESCE(ev.value->>'kind', '') AS kind,
    COALESCE(ev.value->>'retry_filter_event_type', '') AS event_type,
    COALESCE(ev.value->>'retry_filter_retry_count', '') AS retry_filter_retry_count,
    COALESCE(ev.value->>'retry_filter_retry_delay_ms', '') AS retry_filter_retry_delay_ms,
    CASE WHEN lower(COALESCE(ev.value->>'retry_filter_behavior_changed', '')) IN ('true', 't')
         THEN true ELSE false END AS behavior_changed
  FROM ops_error_logs e
  CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(e.upstream_errors) = 'array' THEN e.upstream_errors ELSE '[]'::jsonb END
  ) WITH ORDINALITY AS ev(value, ordinality)
  WHERE COALESCE(ev.value->>'retry_filter_rule_id', '') ~ '^[0-9]+$'
    AND (ev.value->>'retry_filter_rule_id')::bigint > 0
), retry_events_enriched AS (
  SELECT
    re.*,
    COALESCE(a.name, '') AS account_name,
    COALESCE(g.name, '') AS group_name,
    COALESCE(p.name, '') AS preset_name,
    COALESCE(r.name, '') AS rule_name,
    COALESCE(
      CASE WHEN COALESCE(re.retry_filter_retry_count, '') ~ '^[0-9]+$'
           THEN re.retry_filter_retry_count::integer END,
      r.retry_count_override
    ) AS retry_count_override,
    COALESCE(
      CASE WHEN COALESCE(re.retry_filter_retry_delay_ms, '') ~ '^[0-9]+$'
           THEN re.retry_filter_retry_delay_ms::integer END,
      r.retry_delay_ms
    ) AS retry_delay_ms,
    (re.event_type IN ('request_empty_body', 'request_empty_structured')) AS empty_request,
    (re.event_type IN ('empty_completed_response', 'empty_interrupted_stream')) AS empty_response
  FROM retry_events re
  LEFT JOIN accounts a ON a.id = re.account_id
  LEFT JOIN groups g ON g.id = re.group_id
  LEFT JOIN retry_filter_presets p ON p.id = re.preset_id
  LEFT JOIN retry_filter_rules r ON r.id = re.rule_id
)
`

func (r *opsRepository) ListRetryFilterMonitorEvents(ctx context.Context, filter *service.RetryFilterMonitorFilter) (*service.RetryFilterMonitorList, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil ops repository")
	}
	if filter == nil {
		filter = &service.RetryFilterMonitorFilter{}
	}
	page, pageSize := filter.Page, filter.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	where, args := buildRetryFilterMonitorWhere(filter)
	countSQL := retryFilterEventsCTE + "SELECT COUNT(*) FROM retry_events_enriched WHERE " + where
	var total int64
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	selectArgs := append([]any{}, args...)
	selectArgs = append(selectArgs, pageSize, (page-1)*pageSize)
	selectSQL := retryFilterEventsCTE + `
SELECT error_id::text || ':' || ordinality::text,
       created_at, request_id, account_id, account_name, group_id, group_name,
       preset_id, preset_name, rule_id, rule_name, action, status_code, model,
       reason, detail, kind, event_type, behavior_changed, retry_count_override,
       retry_delay_ms, empty_request, empty_response
FROM retry_events_enriched
WHERE ` + where + `
ORDER BY created_at DESC, error_id DESC, ordinality DESC
LIMIT $` + itoa(len(args)+1) + ` OFFSET $` + itoa(len(args)+2)

	rows, err := r.db.QueryContext(ctx, selectSQL, selectArgs...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*service.RetryFilterMonitorEvent, 0, pageSize)
	for rows.Next() {
		item := &service.RetryFilterMonitorEvent{}
		var accountID, groupID sql.NullInt64
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.RequestID, &accountID, &item.AccountName, &groupID, &item.GroupName, &item.PresetID, &item.PresetName, &item.RuleID, &item.RuleName, &item.Action, &item.StatusCode, &item.Model, &item.Reason, &item.Detail, &item.Kind, &item.EventType, &item.BehaviorChanged, &item.RetryCount, &item.RetryDelayMs, &item.EmptyRequest, &item.EmptyResponse); err != nil {
			return nil, err
		}
		if accountID.Valid {
			v := accountID.Int64
			item.AccountID = &v
		}
		if groupID.Valid {
			v := groupID.Int64
			item.GroupID = &v
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	byAction := map[string]int64{}
	actionRows, err := r.db.QueryContext(ctx, retryFilterEventsCTE+"SELECT action, COUNT(*) FROM retry_events_enriched WHERE "+where+" GROUP BY action", args...)
	if err == nil {
		defer func() { _ = actionRows.Close() }()
		for actionRows.Next() {
			var action string
			var count int64
			if scanErr := actionRows.Scan(&action, &count); scanErr != nil {
				return nil, scanErr
			}
			byAction[action] = count
		}
		if scanErr := actionRows.Err(); scanErr != nil {
			return nil, scanErr
		}
	}
	if err != nil {
		return nil, err
	}

	return &service.RetryFilterMonitorList{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Pages:    retryFilterMonitorPages(total, pageSize),
		Summary: service.RetryFilterMonitorSummary{
			Matched:      total,
			Retries:      byAction[model.RetryFilterActionSameAccountRetry],
			Terminates:   byAction[model.RetryFilterActionTerminate],
			ByAction:     byAction,
			RecentErrors: items[:minRetryFilterMonitorItems(len(items), 10)],
		},
	}, nil
}

func (r *opsRepository) GetRetryFilterMonitorEvent(ctx context.Context, id string) (*service.RetryFilterMonitorEvent, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil ops repository")
	}
	parts := strings.SplitN(strings.TrimSpace(id), ":", 2)
	if len(parts) != 2 {
		return nil, nil
	}
	errorID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || errorID <= 0 {
		return nil, nil
	}
	ordinality, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || ordinality <= 0 {
		return nil, nil
	}
	q := retryFilterEventsCTE + `
SELECT error_id::text || ':' || ordinality::text,
       created_at, request_id, account_id, account_name, group_id, group_name,
       preset_id, preset_name, rule_id, rule_name, action, status_code, model,
       reason, detail, kind, event_type, behavior_changed, retry_count_override,
       retry_delay_ms, empty_request, empty_response
FROM retry_events_enriched
WHERE error_id = $1 AND ordinality = $2
LIMIT 1`
	item := &service.RetryFilterMonitorEvent{}
	var accountID, groupID sql.NullInt64
	err = r.db.QueryRowContext(ctx, q, errorID, ordinality).Scan(&item.ID, &item.CreatedAt, &item.RequestID, &accountID, &item.AccountName, &groupID, &item.GroupName, &item.PresetID, &item.PresetName, &item.RuleID, &item.RuleName, &item.Action, &item.StatusCode, &item.Model, &item.Reason, &item.Detail, &item.Kind, &item.EventType, &item.BehaviorChanged, &item.RetryCount, &item.RetryDelayMs, &item.EmptyRequest, &item.EmptyResponse)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if accountID.Valid {
		v := accountID.Int64
		item.AccountID = &v
	}
	if groupID.Valid {
		v := groupID.Int64
		item.GroupID = &v
	}
	return item, nil
}

func minRetryFilterMonitorItems(length, limit int) int {
	if length < limit {
		return length
	}
	return limit
}

func retryFilterMonitorPages(total int64, pageSize int) int {
	if pageSize <= 0 || total <= 0 {
		return 1
	}
	pages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if pages < 1 {
		return 1
	}
	return pages
}

func buildRetryFilterMonitorWhere(filter *service.RetryFilterMonitorFilter) (string, []any) {
	clauses := []string{"1=1"}
	args := make([]any, 0, 10)
	if filter == nil {
		return strings.Join(clauses, " AND "), args
	}
	if filter.StartTime != nil && !filter.StartTime.IsZero() {
		args = append(args, filter.StartTime.UTC())
		clauses = append(clauses, "created_at >= $"+itoa(len(args)))
	}
	if filter.EndTime != nil && !filter.EndTime.IsZero() {
		args = append(args, filter.EndTime.UTC())
		clauses = append(clauses, "created_at < $"+itoa(len(args)))
	}
	if action := strings.TrimSpace(filter.Action); action != "" {
		args = append(args, action)
		clauses = append(clauses, "action = $"+itoa(len(args)))
	}
	if filter.StatusCode != nil {
		args = append(args, *filter.StatusCode)
		clauses = append(clauses, "status_code = $"+itoa(len(args)))
	}
	if model := strings.TrimSpace(filter.Model); model != "" {
		args = append(args, "%"+escapeLikePattern(model)+"%")
		clauses = append(clauses, "model ILIKE $"+itoa(len(args)))
	}
	if filter.AccountID != nil && *filter.AccountID > 0 {
		args = append(args, *filter.AccountID)
		clauses = append(clauses, "account_id = $"+itoa(len(args)))
	}
	if filter.GroupID != nil && *filter.GroupID > 0 {
		args = append(args, *filter.GroupID)
		clauses = append(clauses, "group_id = $"+itoa(len(args)))
	}
	if filter.PresetID != nil && *filter.PresetID > 0 {
		args = append(args, *filter.PresetID)
		clauses = append(clauses, "preset_id = $"+itoa(len(args)))
	}
	if filter.RuleID != nil && *filter.RuleID > 0 {
		args = append(args, *filter.RuleID)
		clauses = append(clauses, "rule_id = $"+itoa(len(args)))
	}
	if reason := strings.TrimSpace(filter.Reason); reason != "" {
		args = append(args, "%"+escapeLikePattern(reason)+"%")
		clauses = append(clauses, "reason ILIKE $"+itoa(len(args)))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		args = append(args, "%"+escapeLikePattern(query)+"%")
		n := itoa(len(args))
		clauses = append(clauses, "(request_id ILIKE $"+n+" OR model ILIKE $"+n+" OR reason ILIKE $"+n+" OR account_name ILIKE $"+n+" OR group_name ILIKE $"+n+" OR preset_name ILIKE $"+n+" OR rule_name ILIKE $"+n+")")
	}
	return strings.Join(clauses, " AND "), args
}
