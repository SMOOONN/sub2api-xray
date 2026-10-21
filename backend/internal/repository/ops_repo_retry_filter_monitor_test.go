package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func retryMonitorTestRepository(t *testing.T) *opsRepository {
	t.Helper()
	dsn := os.Getenv("SUB2API_RETRY_MONITOR_TEST_DSN")
	if dsn == "" {
		t.Skip("SUB2API_RETRY_MONITOR_TEST_DSN is required for the PostgreSQL monitor contract")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(ctx))
	_, err = db.ExecContext(ctx, `
CREATE TEMP TABLE ops_error_logs (
  id bigint PRIMARY KEY, created_at timestamptz NOT NULL, request_id text,
  account_id bigint, group_id bigint, requested_model text, model text, upstream_errors jsonb
);
CREATE TEMP TABLE accounts (id bigint PRIMARY KEY, name text);
CREATE TEMP TABLE groups (id bigint PRIMARY KEY, name text);
CREATE TEMP TABLE retry_filter_presets (id bigint PRIMARY KEY, name text);
CREATE TEMP TABLE retry_filter_rules (id bigint PRIMARY KEY, name text, retry_count_override integer, retry_delay_ms integer);
INSERT INTO retry_filter_presets VALUES (3, 'timing');
INSERT INTO retry_filter_rules VALUES (7, 'retry timing', 15, 1000);
`)
	require.NoError(t, err)
	return &opsRepository{db: db}
}

func insertRetryMonitorTestLog(t *testing.T, repo *opsRepository, id int64, parent time.Time, timestamps ...any) {
	t.Helper()
	events := make([]map[string]any, 0, len(timestamps))
	for _, timestamp := range timestamps {
		event := map[string]any{
			"retry_filter_preset_id": 3, "retry_filter_rule_id": 7,
			"retry_filter_action": "same_account_retry", "upstream_status_code": 429,
			"retry_filter_retry_count": 15, "retry_filter_retry_delay_ms": 1000,
		}
		if timestamp != nil {
			event["at_unix_ms"] = timestamp
		}
		events = append(events, event)
	}
	payload, err := json.Marshal(events)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(context.Background(), `
INSERT INTO ops_error_logs (id, created_at, request_id, model, upstream_errors)
VALUES ($1, $2, $3, 'timing-model', $4::jsonb)`, id, parent, fmt.Sprintf("timing-request-%d", id), string(payload))
	require.NoError(t, err)
}

func TestRetryFilterMonitorEventTimestamps(t *testing.T) {
	repo := retryMonitorTestRepository(t)
	ctx := context.Background()
	first := time.Date(2026, 10, 12, 11, 9, 26, 842000000, time.UTC)
	second, third, last := first.Add(1534*time.Millisecond), first.Add(3077*time.Millisecond), first.Add(23240*time.Millisecond)
	parent := last.Add(9 * time.Millisecond)
	insertRetryMonitorTestLog(t, repo, 481, parent, first.UnixMilli(), second.UnixMilli(), last.UnixMilli())
	// Parent-log order deliberately differs from attempt order across requests.
	insertRetryMonitorTestLog(t, repo, 900, first.Add(-time.Second), third.UnixMilli())

	list, err := repo.ListRetryFilterMonitorEvents(ctx, &service.RetryFilterMonitorFilter{PageSize: 10})
	require.NoError(t, err)
	require.EqualValues(t, 4, list.Total)
	require.Len(t, list.Items, 4)
	require.EqualValues(t, 4, list.Summary.Retries)
	for i, want := range []struct {
		id string
		at time.Time
	}{{"481:3", last}, {"900:1", third}, {"481:2", second}, {"481:1", first}} {
		item := list.Items[i]
		require.Equal(t, want.id, item.ID)
		require.True(t, want.at.Equal(item.CreatedAt), "event %s: got %s, want %s", item.ID, item.CreatedAt, want.at)
		require.Equal(t, item.CreatedAt, list.Summary.RecentErrors[i].CreatedAt)
		require.NotNil(t, item.RetryCount)
		require.Equal(t, 15, *item.RetryCount)
		require.NotNil(t, item.RetryDelayMs)
		require.Equal(t, 1000, *item.RetryDelayMs)
		detail, detailErr := repo.GetRetryFilterMonitorEvent(ctx, item.ID)
		require.NoError(t, detailErr)
		require.NotNil(t, detail)
		require.True(t, detail.CreatedAt.Equal(want.at))
		encoded, encodeErr := json.Marshal(detail)
		require.NoError(t, encodeErr)
		var apiItem struct {
			CreatedAt time.Time `json:"created_at"`
		}
		require.NoError(t, json.Unmarshal(encoded, &apiItem))
		require.True(t, apiItem.CreatedAt.Equal(want.at))
	}

	window, err := repo.ListRetryFilterMonitorEvents(ctx, &service.RetryFilterMonitorFilter{StartTime: &second, EndTime: &third})
	require.NoError(t, err)
	require.EqualValues(t, 1, window.Total)
	require.Len(t, window.Items, 1)
	require.Equal(t, "481:2", window.Items[0].ID)
	require.EqualValues(t, 1, window.Summary.Retries)

	page1, err := repo.ListRetryFilterMonitorEvents(ctx, &service.RetryFilterMonitorFilter{Page: 1, PageSize: 2})
	require.NoError(t, err)
	page2, err := repo.ListRetryFilterMonitorEvents(ctx, &service.RetryFilterMonitorFilter{Page: 2, PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, 2, page1.Pages)
	require.Equal(t, 2, page2.Pages)
	for i, item := range append(page1.Items, page2.Items...) {
		require.Equal(t, list.Items[i].ID, item.ID)
	}
	var storedParent time.Time
	var storedEvents json.RawMessage
	require.NoError(t, repo.db.QueryRowContext(ctx, "SELECT created_at, upstream_errors FROM ops_error_logs WHERE id = 481").Scan(&storedParent, &storedEvents))
	require.True(t, parent.Equal(storedParent))
	var original []struct {
		AtUnixMs int64 `json:"at_unix_ms"`
	}
	require.NoError(t, json.Unmarshal(storedEvents, &original))
	require.Equal(t, []int64{first.UnixMilli(), second.UnixMilli(), last.UnixMilli()}, []int64{original[0].AtUnixMs, original[1].AtUnixMs, original[2].AtUnixMs})
}

func TestRetryFilterMonitorLegacyTimestampFallback(t *testing.T) {
	repo := retryMonitorTestRepository(t)
	ctx := context.Background()
	parent := time.Date(2026, 10, 12, 11, 9, 50, 91000000, time.UTC)
	invalid := []any{nil, json.RawMessage("null"), 0, -1, "", "invalid", 1.5, true, map[string]any{"ms": 1}, "9223372036854775808", "253402300800000"}
	insertRetryMonitorTestLog(t, repo, 1, parent, invalid...)
	valid := parent.Add(-time.Second)
	insertRetryMonitorTestLog(t, repo, 2, parent, fmt.Sprint(valid.UnixMilli()))
	list, err := repo.ListRetryFilterMonitorEvents(ctx, &service.RetryFilterMonitorFilter{PageSize: 100})
	require.NoError(t, err)
	require.EqualValues(t, len(invalid)+1, list.Total)
	for ordinal := range invalid {
		detail, detailErr := repo.GetRetryFilterMonitorEvent(ctx, fmt.Sprintf("1:%d", ordinal+1))
		require.NoError(t, detailErr)
		require.NotNil(t, detail)
		require.True(t, parent.Equal(detail.CreatedAt), "legacy event %d must retain the only available time", ordinal+1)
	}
	detail, err := repo.GetRetryFilterMonitorEvent(ctx, "2:1")
	require.NoError(t, err)
	require.True(t, valid.Equal(detail.CreatedAt))
}
