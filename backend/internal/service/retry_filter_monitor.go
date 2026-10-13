package service

import (
	"context"
	"time"
)

// RetryFilterMonitorFilter describes the admin monitor query. The source is
// the existing ops_error_logs.upstream_errors JSON stream; no second audit
// table is needed.
type RetryFilterMonitorFilter struct {
	StartTime  *time.Time
	EndTime    *time.Time
	Action     string
	StatusCode *int
	Model      string
	AccountID  *int64
	GroupID    *int64
	PresetID   *int64
	RuleID     *int64
	Reason     string
	Query      string
	Page       int
	PageSize   int
}

type RetryFilterMonitorEvent struct {
	ID              string    `json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	RequestID       string    `json:"request_id,omitempty"`
	AccountID       *int64    `json:"account_id,omitempty"`
	AccountName     string    `json:"account_name,omitempty"`
	GroupID         *int64    `json:"group_id,omitempty"`
	GroupName       string    `json:"group_name,omitempty"`
	PresetID        int64     `json:"preset_id,omitempty"`
	PresetName      string    `json:"preset_name,omitempty"`
	RuleID          int64     `json:"rule_id,omitempty"`
	RuleName        string    `json:"rule_name,omitempty"`
	Action          string    `json:"action,omitempty"`
	StatusCode      int       `json:"status_code,omitempty"`
	Model           string    `json:"model,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	Detail          string    `json:"detail,omitempty"`
	Kind            string    `json:"kind,omitempty"`
	EventType       string    `json:"event_type,omitempty"`
	BehaviorChanged bool      `json:"behavior_changed,omitempty"`
	RetryCount      *int      `json:"retry_count,omitempty"`
	RetryDelayMs    *int      `json:"retry_delay_ms,omitempty"`
	EmptyRequest    bool      `json:"empty_request,omitempty"`
	EmptyResponse   bool      `json:"empty_response,omitempty"`
}

type RetryFilterMonitorSummary struct {
	Matched      int64                      `json:"matched"`
	Retries      int64                      `json:"retries"`
	Terminates   int64                      `json:"terminates"`
	ByAction     map[string]int64           `json:"by_action"`
	RecentErrors []*RetryFilterMonitorEvent `json:"recent_errors"`
}

type RetryFilterMonitorList struct {
	Items    []*RetryFilterMonitorEvent `json:"items"`
	Total    int64                      `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"page_size"`
	Pages    int                        `json:"pages"`
	Summary  RetryFilterMonitorSummary  `json:"summary"`
}

type retryFilterMonitorRepository interface {
	ListRetryFilterMonitorEvents(context.Context, *RetryFilterMonitorFilter) (*RetryFilterMonitorList, error)
}

type retryFilterMonitorDetailRepository interface {
	GetRetryFilterMonitorEvent(context.Context, string) (*RetryFilterMonitorEvent, error)
}

// ListRetryFilterMonitorEvents exposes retry-filter annotations already stored
// by Ops. A missing/older repository implementation degrades to an empty
// result, which keeps the management page available during rolling upgrades.
func (s *OpsService) ListRetryFilterMonitorEvents(ctx context.Context, filter *RetryFilterMonitorFilter) (*RetryFilterMonitorList, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	page, pageSize := 1, 20
	if filter != nil {
		if filter.Page > 0 {
			page = filter.Page
		}
		if filter.PageSize > 0 {
			pageSize = filter.PageSize
		}
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if source, ok := s.opsRepo.(retryFilterMonitorRepository); ok {
		if filter == nil {
			filter = &RetryFilterMonitorFilter{}
		}
		filterCopy := *filter
		filterCopy.Page, filterCopy.PageSize = page, pageSize
		return source.ListRetryFilterMonitorEvents(ctx, &filterCopy)
	}
	return &RetryFilterMonitorList{
		Items:    []*RetryFilterMonitorEvent{},
		Total:    0,
		Page:     page,
		PageSize: pageSize,
		Pages:    1,
		Summary:  RetryFilterMonitorSummary{ByAction: map[string]int64{}, RecentErrors: []*RetryFilterMonitorEvent{}},
	}, nil
}

func (s *OpsService) GetRetryFilterMonitorEvent(ctx context.Context, id string) (*RetryFilterMonitorEvent, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if source, ok := s.opsRepo.(retryFilterMonitorDetailRepository); ok {
		return source.GetRetryFilterMonitorEvent(ctx, id)
	}
	return nil, nil
}
