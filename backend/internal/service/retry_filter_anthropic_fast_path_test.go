//go:build unit

package service

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

func TestRetryFilterAnthropicFastPathDistinguishesVisibleOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		delta   string
		visible bool
	}{
		{"text", `{"type":"text_delta","text":"answer"}`, true},
		{"thinking", `{"type":"thinking_delta","thinking":"reasoning"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{
				EmptyResponseMode: model.RetryFilterEmptyResponseBoth,
				Action:            model.RetryFilterActionSameAccountRetry,
			})
			c, rec, account := retryFilterAttemptContextForTest(runtime)
			body := "event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":` + tc.delta + "}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
			_, err := newMinimalGatewayService().handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "model", "model", false)
			if tc.visible {
				require.NoError(t, err)
				require.Contains(t, rec.Body.String(), "answer")
				return
			}
			var candidate *UpstreamFailoverError
			require.ErrorAs(t, err, &candidate)
			require.Equal(t, RetryFilterEventEmptyCompletedResponse, candidate.RetryFilterEventType)
			require.Empty(t, rec.Body.String())
		})
	}
}
