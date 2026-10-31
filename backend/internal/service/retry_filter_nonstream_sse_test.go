//go:build unit

package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The new non-completed SSE-to-JSON path must preserve the retry filter's
// completed-only boundary and still intercept a genuinely empty completion.
func TestRetryFilterNonStreamingSSETerminals(t *testing.T) {
	for _, protocol := range []string{"responses", "responses-passthrough"} {
		for _, status := range []string{"completed", "incomplete", "cancelled", "canceled"} {
			for _, kind := range []string{"empty", "text"} {
				t.Run(protocol+"/"+status+"/"+kind, func(t *testing.T) {
					rule := &model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry}
					c, rec, account := retryFilterAttemptContextForTest(retryFilterAttemptRuntimeForTest(rule))
					c.Request.URL.Path = "/v1/responses"
					payload := strings.Replace(retryFilterJSONFixture(protocol, kind), `"status":"completed"`, `"status":"`+status+`"`, 1)
					body := "event: response." + status + "\ndata: {\"type\":\"response." + status + "\",\"response\":" + payload + "}\n\n"
					resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
					input, output, err := callRetryFilterJSONConsumer(protocol, c, account, resp)
					if status == "completed" && kind == "empty" {
						var candidate *UpstreamFailoverError
						require.ErrorAs(t, err, &candidate)
						require.Equal(t, RetryFilterEventEmptyCompletedResponse, candidate.RetryFilterEventType)
						require.False(t, c.Writer.Written())
						require.Empty(t, rec.Body.String())
						return
					}
					require.NoError(t, err)
					require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
					require.True(t, gjson.ValidBytes(rec.Body.Bytes()))
					require.Equal(t, status, gjson.GetBytes(rec.Body.Bytes(), "status").String())
					require.Equal(t, gjson.Get(payload, "output").Raw, gjson.GetBytes(rec.Body.Bytes(), "output").Raw)
					require.Equal(t, 11, input)
					require.Equal(t, 7, output)
					require.Empty(t, retryFilterAttemptEvents(t, c))
				})
			}
		}
	}
}
