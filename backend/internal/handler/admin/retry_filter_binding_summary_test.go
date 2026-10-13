package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type bindingSummaryAdminStub struct {
	service.AdminService
	accounts []service.Account
	groups   []service.Group
}

func (s *bindingSummaryAdminStub) ListAccountsForSchedulerScoreFilter(context.Context, string, string, string, string, int64, string) ([]service.Account, error) {
	return s.accounts, nil
}

func (s *bindingSummaryAdminStub) GetAllGroupsIncludingInactive(context.Context) ([]service.Group, error) {
	return s.groups, nil
}

func TestRetryFilterBindingSummaryPreservesJSONAndSelection(t *testing.T) {
	presetID := int64(7)
	stub := &bindingSummaryAdminStub{
		accounts: []service.Account{
			{ID: 11, Credentials: map[string]any{"retry_filter_preset_id": presetID}},
			{ID: 12, Credentials: map[string]any{"retry_filter_preset_id": int64(8)}},
		},
		groups: []service.Group{
			{ID: 21, RetryFilterConfig: service.GroupRetryFilterConfig{PresetID: &presetID}},
			{ID: 22},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	h := &RetryFilterHandler{admin: stub}
	for _, tc := range []struct {
		id   int64
		want string
	}{
		{7, `{"account_ids":[11],"group_ids":[21],"account_count":1,"group_count":1,"global":false}`},
		{9, `{"account_ids":[],"group_ids":[],"account_count":0,"group_count":0,"global":false}`},
	} {
		summary, err := h.bindingSummary(c, tc.id)
		require.NoError(t, err)
		body, err := json.Marshal(summary)
		require.NoError(t, err)
		require.JSONEq(t, tc.want, string(body))
	}
}
