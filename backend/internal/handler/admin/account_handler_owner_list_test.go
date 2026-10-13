package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountHandlerListPreservesResourceOwners(t *testing.T) {
	ownerA, ownerB := int64(7), int64(9)
	for _, tc := range []struct {
		name  string
		query string
	}{
		{name: "full"},
		{name: "lite", query: "&lite=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, adminSvc := setupAccountListRouter()
			adminSvc.accounts = []service.Account{
				{ID: 501, Name: "system", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive},
				{ID: 502, Name: "user-a", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, OwnerUserID: &ownerA},
				{ID: 503, Name: "user-b", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, OwnerUserID: &ownerB},
			}

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts?page=1&page_size=20"+tc.query, nil)
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)

			var payload struct {
				Data struct {
					Items []map[string]any `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
			require.Len(t, payload.Data.Items, 3)
			byID := make(map[int64]map[string]any, 3)
			for _, item := range payload.Data.Items {
				id, ok := item["id"].(float64)
				require.True(t, ok)
				byID[int64(id)] = item
			}
			require.NotContains(t, byID[501], "owner_user_id")
			require.Equal(t, float64(ownerA), byID[502]["owner_user_id"])
			require.Equal(t, float64(ownerB), byID[503]["owner_user_id"])
		})
	}
}

func TestAccountHandlerListLiteOwnerChangeInvalidatesETag(t *testing.T) {
	router, adminSvc := setupAccountListRouter()
	ownerA, ownerB := int64(7), int64(9)
	adminSvc.accounts = []service.Account{{
		ID: 501, Name: "owned-account", Platform: service.PlatformOpenAI,
		Type: service.AccountTypeAPIKey, Status: service.StatusActive, OwnerUserID: &ownerA,
	}}
	const path = "/api/v1/admin/accounts?page=1&page_size=20&lite=1"
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, first.Code)
	firstETag := first.Header().Get("ETag")
	require.NotEmpty(t, firstETag)

	// A changed owner must update the displayed row even if other fields are identical.
	adminSvc.accounts[0].OwnerUserID = &ownerB
	changed := httptest.NewRecorder()
	changedRequest := httptest.NewRequest(http.MethodGet, path, nil)
	changedRequest.Header.Set("If-None-Match", firstETag)
	router.ServeHTTP(changed, changedRequest)
	require.Equal(t, http.StatusOK, changed.Code)
	require.NotEqual(t, firstETag, changed.Header().Get("ETag"))

	var payload struct {
		Data struct {
			Items []struct {
				OwnerUserID *int64 `json:"owner_user_id"`
			} `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(changed.Body.Bytes(), &payload))
	require.Len(t, payload.Data.Items, 1)
	require.NotNil(t, payload.Data.Items[0].OwnerUserID)
	require.Equal(t, ownerB, *payload.Data.Items[0].OwnerUserID)

	unchanged := httptest.NewRecorder()
	unchangedRequest := httptest.NewRequest(http.MethodGet, path, nil)
	unchangedRequest.Header.Set("If-None-Match", changed.Header().Get("ETag"))
	router.ServeHTTP(unchanged, unchangedRequest)
	require.Equal(t, http.StatusNotModified, unchanged.Code)
}
