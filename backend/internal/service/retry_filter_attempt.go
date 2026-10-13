package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const RetryFilterTerminateReason GatewayFailureReason = "retry_filter_terminate"

type retryFilterAttemptKey struct{}

// retryFilterAttempt belongs to one selected route, never to an account's
// arbitrary group membership. Handlers replace it before every Forward call.
type retryFilterAttempt struct {
	runtime   *RetryFilterRuntime
	group     *Group
	groupID   int64
	model     string
	pending   *UpstreamFailoverError
	accountID int64
}

func WithRetryFilterAttempt(ctx context.Context, runtime *RetryFilterRuntime, group *Group, groupID int64, modelName string) context.Context {
	return context.WithValue(ctx, retryFilterAttemptKey{}, &retryFilterAttempt{
		runtime: runtime, group: group, groupID: groupID, model: modelName,
	})
}

func retryFilterAttemptFromContext(ctx context.Context) *retryFilterAttempt {
	if ctx == nil {
		return nil
	}
	attempt, _ := ctx.Value(retryFilterAttemptKey{}).(*retryFilterAttempt)
	return attempt
}

type RetryFilterInput struct {
	Account     *Account
	Group       *Group
	GroupID     int64
	Model       string
	RetryLimit  int
	FailoverErr *UpstreamFailoverError
}

func (r *RetryFilterRuntime) requestGroup(ctx context.Context, group *Group, groupID int64) *Group {
	if group != nil && (groupID <= 0 || group.ID == groupID) {
		return group
	}
	if r != nil && r.groupLookup != nil && groupID > 0 {
		group, _ = r.groupLookup(ctx, groupID)
		return group
	}
	return nil
}

func (d *RetryFilterDecision) Actionable() bool {
	return d != nil && d.Matched && d.Rule != nil &&
		(d.Action == model.RetryFilterActionSameAccountRetry || d.Action == model.RetryFilterActionSwitchAccount || d.Action == model.RetryFilterActionTerminate)
}

// Prepare is the sole owner of rule mutations. It does not retry, sleep, write
// a response, penalize an account or append an Ops attempt.
func (r *RetryFilterRuntime) Prepare(ctx context.Context, input RetryFilterInput) *RetryFilterDecision {
	err := input.FailoverErr
	if err == nil || input.Account == nil {
		return nil
	}
	if err.retryFilterEvaluated {
		return err.retryFilterDecision
	}
	err.retryFilterEvaluated = true
	decision := r.Evaluate(ctx, input.Account, r.requestGroup(ctx, input.Group, input.GroupID), input.Model, err)
	if decision == nil || !decision.Matched || decision.Rule == nil {
		return nil
	}
	err.retryFilterDecision = decision
	// Empty requests are local validation events. Only terminate may change
	// their business result; retry/switch remain observations.
	if (err.RetryFilterEventType == RetryFilterEventRequestEmptyBody || err.RetryFilterEventType == RetryFilterEventRequestEmptyStructured) && decision.Action != model.RetryFilterActionTerminate {
		return decision
	}
	switch decision.Action {
	case model.RetryFilterActionSameAccountRetry:
		err.retryFilterChanged = !err.RetryableOnSameAccount || err.NextAccountAction == NextAccountStop || (decision.Rule.RetryCountOverride != nil && *decision.Rule.RetryCountOverride != input.RetryLimit) || decision.Rule.RetryDelayMs != nil
		err.RetryableOnSameAccount = true
		err.NextAccountAction = NextAccountRetry
	case model.RetryFilterActionSwitchAccount:
		err.retryFilterChanged = err.RetryableOnSameAccount || !err.SuppressAccountPenalty || err.NextAccountAction == NextAccountStop
		err.RetryableOnSameAccount = false
		err.SuppressAccountPenalty = true
		err.NextAccountAction = NextAccountRetry
	case model.RetryFilterActionTerminate:
		err.retryFilterChanged = err.NextAccountAction != NextAccountStop || err.Reason != RetryFilterTerminateReason
		err.RetryableOnSameAccount = false
		err.NextAccountAction = NextAccountStop
		err.Reason = RetryFilterTerminateReason
		err.ClientStatusCode = err.StatusCode
		if decision.Rule.TerminateStatusCode != nil {
			err.ClientStatusCode = *decision.Rule.TerminateStatusCode
		}
		if err.ClientStatusCode < 400 || err.ClientStatusCode > 599 {
			err.ClientStatusCode = http.StatusBadGateway
		}
		err.ClientMessage = sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(err.ResponseBody))
		if decision.Rule.TerminateMessage != nil {
			err.ClientMessage = sanitizeUpstreamErrorMessage(*decision.Rule.TerminateMessage)
		}
	case model.RetryFilterActionObserve:
		// All legacy retry/penalty/response metadata is retained.
	default:
		err.retryFilterDecision = nil
		return nil
	}
	return decision
}

func (r *RetryFilterRuntime) Apply(ctx context.Context, c *gin.Context, input RetryFilterInput) *RetryFilterDecision {
	decision := r.Prepare(ctx, input)
	recordRetryFilterDecision(c, input.Account, input.FailoverErr)
	return decision
}

func prepareRetryFilterFailure(ctx context.Context, c *gin.Context, account *Account, err *UpstreamFailoverError) *RetryFilterDecision {
	attempt := retryFilterAttemptFromContext(ctx)
	if attempt == nil || account == nil || err == nil {
		return nil
	}
	decision := attempt.runtime.Prepare(ctx, RetryFilterInput{
		Account: account, Group: attempt.group, GroupID: attempt.groupID,
		Model: attempt.model, RetryLimit: account.GetPoolModeRetryCount(), FailoverErr: err,
	})
	err.retryFilterPreparedInService = true
	attempt.pending, attempt.accountID = err, account.ID
	return decision
}

// prepareGatewayHTTPFailure gives every upstream HTTP error, including codes
// outside the legacy failover whitelist, one decision before side effects or
// response writing. The same body snapshot remains available to legacy code.
func (s *GatewayService) prepareGatewayHTTPFailure(ctx context.Context, c *gin.Context, account *Account, resp *http.Response) *UpstreamFailoverError {
	attempt := retryFilterAttemptFromContext(ctx)
	if attempt == nil || !attempt.runtime.appliesToAccount(ctx, account) || resp == nil || resp.StatusCode < 400 {
		return nil
	}
	body, _ := s.readUpstreamErrorBody(resp)
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	candidate := &UpstreamFailoverError{
		StatusCode: resp.StatusCode, ResponseHeaders: resp.Header.Clone(), ResponseBody: body,
		RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
	}
	if !s.shouldFailoverUpstreamError(resp.StatusCode) && (resp.StatusCode != 400 || s.cfg == nil || !s.cfg.Gateway.FailoverOn400 || !s.shouldFailoverOn400(body)) {
		candidate.NextAccountAction = NextAccountStop
	}
	prepareRetryFilterFailure(ctx, c, account, candidate)
	return candidate
}

func (r *RetryFilterRuntime) appliesToAccount(ctx context.Context, account *Account) bool {
	return r != nil && r.filters != nil && account != nil && r.enabled != nil && r.enabled(ctx) &&
		(r.scope != RetryFilterScopePoolMode || account.IsPoolMode())
}

func copyRetryFilterEvaluation(dst, src *UpstreamFailoverError) {
	if dst == nil || src == nil {
		return
	}
	dst.retryFilterEvaluated, dst.retryFilterDecision = src.retryFilterEvaluated, src.retryFilterDecision
	dst.retryFilterChanged, dst.retryFilterRecorded = src.retryFilterChanged, src.retryFilterRecorded
	dst.retryFilterPreparedInService, dst.retryFilterOpsEvent = src.retryFilterPreparedInService, src.retryFilterOpsEvent
}

func recordRetryFilterDecision(c *gin.Context, account *Account, err *UpstreamFailoverError) {
	if c == nil || account == nil || err == nil || err.retryFilterRecorded || err.retryFilterDecision == nil {
		return
	}
	d := err.retryFilterDecision
	if ev := err.retryFilterOpsEvent; ev != nil {
		ev.RetryFilterPresetID, ev.RetryFilterRuleID = d.PresetID, d.Rule.ID
		ev.RetryFilterAction, ev.RetryFilterBehaviorChanged = d.Action, err.retryFilterChanged
		ev.RetryFilterRetryCount, ev.RetryFilterRetryDelayMs = d.Rule.RetryCountOverride, d.Rule.RetryDelayMs
		ev.RetryFilterEventType = string(err.RetryFilterEventType)
	} else if err.retryFilterPreparedInService {
		// The current service failure has no legacy event. An older unannotated
		// event, even for the same account/status, belongs to a different failure.
		recordRetryFilterFailure(c, account, err)
		return
	} else {
		RecordOpsRetryFilterMatch(c, account.ID, err.StatusCode, d.PresetID, d.Rule.ID, d.Action, err.retryFilterChanged, d.Rule.RetryCountOverride, d.Rule.RetryDelayMs, string(err.RetryFilterEventType))
	}
	logRetryFilterDecision(c, account.ID, err)
}

func logRetryFilterDecision(c *gin.Context, accountID int64, err *UpstreamFailoverError) {
	if err == nil || err.retryFilterRecorded || err.retryFilterDecision == nil {
		return
	}
	err.retryFilterRecorded = true
	if c == nil || c.Request == nil {
		return
	}
	d := err.retryFilterDecision
	logger.FromContext(c.Request.Context()).Info("gateway.retry_filter_match",
		zap.Int64("account_id", accountID), zap.Int("upstream_status", err.StatusCode),
		zap.Int64("preset_id", d.PresetID), zap.Int64("rule_id", d.Rule.ID),
		zap.String("action", d.Action), zap.String("event_type", string(err.RetryFilterEventType)),
		zap.Bool("behavior_changed", err.retryFilterChanged),
	)
}

// recordRetryFilterFailure is used only when interception replaces the legacy
// failure path, or a new semantic-empty candidate has no legacy Ops event.
func recordRetryFilterFailure(c *gin.Context, account *Account, err *UpstreamFailoverError) {
	if err == nil || account == nil || err.retryFilterRecorded {
		return
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		retryFilterFailure: err,
		ProxyID:            opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
		AccountID: account.ID, AccountName: account.Name, Platform: account.Platform,
		UpstreamStatusCode: err.StatusCode, UpstreamRequestID: err.ResponseHeaders.Get("x-request-id"),
		Kind: "failover", Message: extractUpstreamErrorMessage(err.ResponseBody),
		Stage: string(err.Stage), Scope: string(err.Scope), Reason: string(err.Reason),
	})
	recordRetryFilterDecision(c, account, err)
}

// Track actual downstream failures without treating spool seek/read failures
// as a disconnected client. The shared stage retains its existing error contract.
type retryFilterStageWriter struct {
	target   io.Writer
	writeErr error
}

func (w *retryFilterStageWriter) Write(p []byte) (int, error) {
	n, err := w.target.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.writeErr = err
	}
	return n, err
}

func commitRetryFilterStage(stage *openAIFirstOutputStage, dst io.Writer) (clientWriteFailed bool, err error) {
	writer := &retryFilterStageWriter{target: dst}
	err = stage.CommitTo(writer)
	return writer.writeErr != nil, err
}

// A stream is staged only if the selected preset could change its outcome.
// Status/body conditions are deliberately not evaluated before a failure exists.
func retryFilterShouldStage(ctx context.Context, account *Account) bool {
	a := retryFilterAttemptFromContext(ctx)
	if a == nil || !a.runtime.appliesToAccount(ctx, account) {
		return false
	}
	id := a.runtime.ResolvePresetID(ctx, account, a.runtime.requestGroup(ctx, a.group, a.groupID))
	filters := a.runtime.filters
	filters.localMu.RLock()
	defer filters.localMu.RUnlock()
	if !filters.loaded {
		return false
	}
	for _, rule := range filters.local[id] {
		if !rule.Enabled || (rule.Action != model.RetryFilterActionSameAccountRetry && rule.Action != model.RetryFilterActionSwitchAccount && rule.Action != model.RetryFilterActionTerminate) {
			continue
		}
		requestCondition := strings.TrimSpace(rule.EmptyRequestMode) != "" && rule.EmptyRequestMode != model.RetryFilterEmptyRequestNone
		responseCondition := strings.TrimSpace(rule.EmptyResponseMode) != "" && rule.EmptyResponseMode != model.RetryFilterEmptyResponseNone
		if rule.MatchMode == model.MatchModeAll && (requestCondition || (len(rule.Models) > 0 && !modelMatches(a.model, rule.lowerModels))) {
			continue
		}
		if len(rule.StatusCodes)+len(rule.Keywords)+len(rule.UpstreamCodes)+len(rule.Models) == 0 && !responseCondition {
			continue
		}
		return true
	}
	return false
}

func retryFilterKeepaliveInterval(configuredSeconds int) time.Duration {
	if configuredSeconds > 0 && configuredSeconds < 30 {
		return time.Duration(configuredSeconds) * time.Second
	}
	return 30 * time.Second
}

// prepareRetryFilterEmptyJSON keeps completed JSON responses under the same
// semantic-output and policy owners as streams. Native writing and accounting
// continue unchanged unless a rule explicitly takes over this attempt.
func prepareRetryFilterEmptyJSON(ctx context.Context, c *gin.Context, account *Account, resp *http.Response, body []byte) *UpstreamFailoverError {
	attempt := retryFilterAttemptFromContext(ctx)
	if attempt == nil || !attempt.runtime.appliesToAccount(ctx, account) || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	if (c != nil && isOpenAIResponsesCompactPath(c)) || !retryFilterCompletedJSON(body) || RetryFilterResponseHasValidOutput("", body) {
		return nil
	}
	candidate := newRetryFilterStreamCandidate(resp.Header, body, RetryFilterEventEmptyCompletedResponse, "upstream returned an empty completed response")
	decision := prepareRetryFilterFailure(ctx, c, account, candidate)
	if decision != nil {
		recordRetryFilterFailure(c, account, candidate)
	}
	if decision.Actionable() {
		return candidate
	}
	return nil
}

// This is only an envelope/completion guard, not another output detector.
// Async, compact, malformed and error payloads retain their native handling.
func retryFilterCompletedJSON(body []byte) bool {
	var envelope struct {
		Type       string            `json:"type"`
		Object     string            `json:"object"`
		Status     string            `json:"status"`
		StopReason *string           `json:"stop_reason"`
		Content    []json.RawMessage `json:"content"`
		Output     []json.RawMessage `json:"output"`
		Choices    []struct {
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	switch {
	case envelope.Type == "message":
		return envelope.Content != nil && envelope.StopReason != nil && *envelope.StopReason != ""
	case envelope.Object == "response":
		return envelope.Status == "completed" && envelope.Output != nil
	case envelope.Object == "chat.completion":
		if envelope.Choices == nil {
			return false
		}
		for _, choice := range envelope.Choices {
			if choice.FinishReason == nil || *choice.FinishReason == "" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func newRetryFilterStreamCandidate(headers http.Header, payload []byte, event RetryFilterEventType, message string) *UpstreamFailoverError {
	if len(payload) == 0 {
		payload, _ = json.Marshal(map[string]any{"error": map[string]string{"type": "upstream_error", "message": message}})
	}
	return &UpstreamFailoverError{
		StatusCode: http.StatusBadGateway, ResponseBody: payload, ResponseHeaders: headers.Clone(),
		RetryFilterEventType: event, NextAccountAction: NextAccountStop,
	}
}
