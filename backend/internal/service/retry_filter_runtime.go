package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/model"
)

// RetryFilterRuntime resolves the three-level preset reference and evaluates a
// failover error. It intentionally has no package-level state so request code
// can provide the account and model explicitly.
type RetryFilterRuntime struct {
	filters      *RetryFilterService
	enabled      func(context.Context) bool
	globalPreset func(context.Context) (int64, error)
	groupLookup  func(context.Context, int64) (*Group, error)
	scope        RetryFilterScope
}

type RetryFilterScope string

const RetryFilterScopePoolMode RetryFilterScope = "pool_mode"

type RetryFilterDecision struct {
	Rule       *model.RetryFilterRule
	PresetID   int64
	Action     string
	Matched    bool
	MatchModel string
}

func withRetryFilterEvent(err *UpstreamFailoverError, eventType RetryFilterEventType) *UpstreamFailoverError {
	if err != nil {
		err.RetryFilterEventType = eventType
	}
	return err
}

func NewRetryFilterRuntime(filters *RetryFilterService, enabled func(context.Context) bool, globalPreset func(context.Context) (int64, error)) *RetryFilterRuntime {
	return &RetryFilterRuntime{
		filters:      filters,
		enabled:      enabled,
		globalPreset: globalPreset,
		scope:        RetryFilterScopePoolMode,
	}
}

// SetGroupLookup injects the explicit request-group resolver used by the
// runtime. A zero/unknown group ID never falls back to an arbitrary account
// group because account membership alone is not an unambiguous request scope.
func (r *RetryFilterRuntime) SetGroupLookup(lookup func(context.Context, int64) (*Group, error)) {
	if r != nil {
		r.groupLookup = lookup
	}
}

// SetScope allows a future runtime scope to be enabled without changing the
// request decision entry point. Empty values retain the safe pool-mode scope.
func (r *RetryFilterRuntime) SetScope(scope RetryFilterScope) {
	if r == nil || scope == "" {
		return
	}
	r.scope = scope
}

func (r *RetryFilterRuntime) Evaluate(ctx context.Context, account *Account, group *Group, modelName string, failoverErr *UpstreamFailoverError) *RetryFilterDecision {
	eventType := ""
	if failoverErr != nil {
		eventType = string(failoverErr.RetryFilterEventType)
	}
	return r.EvaluateWithEvent(ctx, account, group, modelName, failoverErr, eventType)
}

// EvaluateWithEvent is the request-aware entry point for semantic empty
// request/response conditions. The failover error remains optional so callers
// can evaluate a preflight request-empty event before an upstream call.
func (r *RetryFilterRuntime) EvaluateWithEvent(ctx context.Context, account *Account, group *Group, modelName string, failoverErr *UpstreamFailoverError, eventType string) *RetryFilterDecision {
	if !r.appliesToAccount(ctx, account) {
		return nil
	}
	presetID := r.ResolvePresetID(ctx, account, group)
	if presetID == 0 {
		return nil
	}
	statusCode := 0
	var body []byte
	if failoverErr != nil {
		statusCode = failoverErr.StatusCode
		body = failoverErr.ResponseBody
	}
	rule := r.filters.MatchRuleWithEvent(presetID, statusCode, body, modelName, eventType)
	if rule == nil {
		return nil
	}
	return &RetryFilterDecision{
		Rule:       rule,
		PresetID:   presetID,
		Action:     rule.Action,
		Matched:    true,
		MatchModel: modelName,
	}
}

// EvaluateRequestEvent evaluates an empty-request condition without requiring
// a synthetic upstream error. It is intentionally side-effect free; handlers
// decide whether a matched action should terminate or merely be observed.
func (r *RetryFilterRuntime) EvaluateRequestEvent(ctx context.Context, account *Account, group *Group, modelName string, eventType RetryFilterEventType) *RetryFilterDecision {
	if eventType != RetryFilterEventRequestEmptyBody && eventType != RetryFilterEventRequestEmptyStructured {
		return nil
	}
	return r.EvaluateWithEvent(ctx, account, group, modelName, nil, string(eventType))
}

// EvaluateForGroupID resolves only the explicitly supplied request group.
func (r *RetryFilterRuntime) EvaluateForGroupID(ctx context.Context, account *Account, groupID int64, modelName string, failoverErr *UpstreamFailoverError) *RetryFilterDecision {
	var group *Group
	if groupID > 0 && r != nil && r.groupLookup != nil {
		group, _ = r.groupLookup(ctx, groupID)
	}
	return r.Evaluate(ctx, account, group, modelName, failoverErr)
}

// ResolvePresetID applies account -> group -> global precedence. Each
// reference is checked for an existing, enabled preset before it is selected;
// invalid or disabled references are skipped so a lower level can take over.
func (r *RetryFilterRuntime) ResolvePresetID(ctx context.Context, account *Account, group *Group) int64 {
	return r.ResolvePreset(ctx, account, group).PresetID
}

type RetryFilterResolvedPreset struct {
	PresetID int64  `json:"preset_id"`
	Source   string `json:"source"`
}

// ResolvePreset exposes the same precedence used by request evaluation.
func (r *RetryFilterRuntime) ResolvePreset(ctx context.Context, account *Account, group *Group) RetryFilterResolvedPreset {
	if r == nil || r.filters == nil {
		return RetryFilterResolvedPreset{Source: "none"}
	}
	if id := retryFilterAccountPresetID(account); id != 0 && r.filters.IsPresetUsable(id) {
		return RetryFilterResolvedPreset{PresetID: id, Source: "account"}
	}
	if group != nil && group.RetryFilterConfig.PresetID != nil {
		if id := *group.RetryFilterConfig.PresetID; id != 0 && r.filters.IsPresetUsable(id) {
			return RetryFilterResolvedPreset{PresetID: id, Source: "group"}
		}
	}
	if r.globalPreset != nil {
		if id, err := r.globalPreset(ctx); err == nil && id != 0 && r.filters.IsPresetUsable(id) {
			return RetryFilterResolvedPreset{PresetID: id, Source: "global"}
		}
	}
	return RetryFilterResolvedPreset{Source: "none"}
}

// RetryFilterAccountPresetID reads only the policy reference, never credentials.
func RetryFilterAccountPresetID(a *Account) int64 { return retryFilterAccountPresetID(a) }

func retryFilterAccountPresetID(a *Account) int64 {
	if a == nil || a.Credentials == nil {
		return 0
	}
	v := a.Credentials["retry_filter_preset_id"]
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		id, _ := strconv.ParseInt(n, 10, 64)
		return id
	}
	return 0
}

// RetryFilterRequestEmptyEvent classifies only the request shapes explicitly
// supported by the filter. The body check is intentionally byte-preserving so
// callers can run it before JSON decoding without consuming the request.
func RetryFilterRequestEmptyEvent(body []byte) RetryFilterEventType {
	if len(strings.TrimSpace(string(body))) == 0 {
		return RetryFilterEventRequestEmptyBody
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return ""
	}
	if retryFilterStructuredRequestEmpty(value) {
		return RetryFilterEventRequestEmptyStructured
	}
	return ""
}

func RetryFilterEmptyRequestErrorBody(eventType RetryFilterEventType) []byte {
	message := "request body is empty"
	if eventType == RetryFilterEventRequestEmptyStructured {
		message = "request content is empty"
	}
	body, err := json.Marshal(map[string]any{
		"error": map[string]string{
			"type":    "invalid_request_error",
			"message": message,
		},
	})
	if err != nil {
		return []byte(`{"error":{"type":"invalid_request_error","message":"request body is empty"}}`)
	}
	return body
}

func retryFilterStructuredRequestEmpty(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case []any:
		if len(v) == 0 {
			return true
		}
		for _, item := range v {
			if retryFilterInputValueHasContent(item) {
				return false
			}
		}
		return true
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if len(obj) == 0 {
		return true
	}
	foundInputField := false
	for _, key := range []string{"messages", "input", "contents", "prompt", "parts"} {
		if raw, exists := obj[key]; exists {
			foundInputField = true
			if retryFilterInputValueHasContent(raw) {
				return false
			}
		}
	}
	return foundInputField
}

// retryFilterInputValueHasContent reports whether a supported request input
// contains actual user/tool content. Request metadata such as model, role,
// type, and generation options are intentionally ignored. This prevents a
// request with one empty field (for example messages:[]) and another populated
// field (for example input:[{text:"hello"}]) from being classified as empty.
func retryFilterInputValueHasContent(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case float64, bool:
		return true
	case []any:
		for _, item := range v {
			if retryFilterInputValueHasContent(item) {
				return true
			}
		}
		return false
	case map[string]any:
		for _, key := range []string{
			"text", "content", "parts", "input", "prompt", "output", "arguments",
			"image_url", "image", "audio", "audio_url", "file_id", "data", "url",
		} {
			if raw, exists := v[key]; exists && retryFilterInputValueHasContent(raw) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// RetryFilterResponseHasValidOutput recognizes semantic output across the
// supported SSE/JSON dialects. Role declarations, usage, heartbeats,
// reasoning-only events and terminal markers intentionally do not count.
func RetryFilterResponseHasValidOutput(eventType string, payload []byte) bool {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	if (eventType != "" && retryFilterNonOutputEvent(eventType)) || strings.EqualFold(strings.TrimSpace(string(payload)), "[DONE]") {
		return false
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return false
	}
	return retryFilterValueHasValidOutput(eventType, value)
}

func retryFilterNonOutputEvent(eventType string) bool {
	if eventType == "" {
		return false
	}
	for _, marker := range []string{"reasoning", "thinking", "usage", "heartbeat", "role"} {
		if strings.Contains(eventType, marker) {
			return true
		}
	}
	switch eventType {
	case "[done]", "message_stop", "response.failed", "error", "ping", "keepalive":
		return true
	default:
		return false
	}
}

func retryFilterValueHasValidOutput(eventType string, value any) bool {
	switch v := value.(type) {
	case map[string]any:
		return retryFilterObjectHasValidOutput(eventType, v)
	case []any:
		for _, item := range v {
			if retryFilterValueHasValidOutput(eventType, item) {
				return true
			}
		}
	}
	return false
}

func retryFilterObjectHasValidOutput(eventType string, obj map[string]any) bool {
	kind, _ := obj["type"].(string)
	kind = strings.ToLower(strings.TrimSpace(kind))
	if thought, ok := obj["thought"].(bool); ok && thought {
		return false
	}
	if strings.HasPrefix(kind, "input_") && kind != "input_json_delta" {
		return false
	}
	if kind != "" && retryFilterNonOutputEvent(kind) {
		return false
	}
	if kind != "" && retryFilterSemanticItem(kind, obj) {
		return true
	}

	// Text-like fields are checked recursively so Anthropic deltas such as
	// {"delta":{"type":"text_delta","text":"..."}} are recognized.
	for _, key := range []string{"text", "output_text", "transcript", "content", "delta", "output", "input", "partial_json", "arguments", "b64_json", "partial_image_b64"} {
		if raw, exists := obj[key]; exists && retryFilterTextValue(raw, eventType) {
			return true
		}
	}
	for _, key := range []string{"tool_calls", "function_call", "tool_use", "function", "functionCall", "image", "images", "inlineData", "fileData", "audio", "output_audio"} {
		if raw, exists := obj[key]; exists && retryFilterArtifactValue(raw, eventType) {
			return true
		}
	}
	for _, key := range []string{"response", "message", "content_block", "item", "part", "parts", "choices", "candidates"} {
		if raw, exists := obj[key]; exists && retryFilterValueHasValidOutput(eventType, raw) {
			return true
		}
	}
	return false
}

// retryFilterSemanticItem handles typed output items before generic recursion.
// A tool/function item is valid when it carries a callable identity or result;
// an empty structural object must not count as output.
func retryFilterSemanticItem(kind string, obj map[string]any) bool {
	switch kind {
	case "text", "text_delta", "output_text", "refusal", "transcript", "audio_transcript_delta", "audio_transcript":
		return retryFilterTextValue(obj["text"], kind) || retryFilterTextValue(obj["transcript"], kind) || retryFilterTextValue(obj["refusal"], kind)
	case "tool_use", "server_tool_use", "mcp_tool_use", "tool_result", "tool_call", "function", "function_call", "custom_tool_call", "computer_call", "file_search_call", "web_search_call", "code_interpreter_call":
		for _, key := range []string{"name", "id", "call_id", "tool_use_id", "arguments", "input", "partial_json", "result", "output", "action", "query", "queries", "content"} {
			if raw, exists := obj[key]; exists && retryFilterTextValue(raw, kind) {
				return true
			}
		}
		return false
	case "image", "image_generation_call", "image_output", "audio", "audio_output", "output_audio":
		for _, key := range []string{"url", "image_url", "source", "b64_json", "data", "result", "output", "audio", "transcript"} {
			if raw, exists := obj[key]; exists && retryFilterArtifactValue(raw, kind) {
				return true
			}
		}
		return false
	case "message":
		if raw, exists := obj["content"]; exists {
			return retryFilterValueHasValidOutput(kind, raw)
		}
		return false
	default:
		return false
	}
}

func retryFilterTextValue(value any, eventType string) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		for _, item := range v {
			if retryFilterTextValue(item, eventType) || retryFilterValueHasValidOutput(eventType, item) {
				return true
			}
		}
	case map[string]any:
		return retryFilterValueHasValidOutput(eventType, v)
	}
	return false
}

// retryFilterArtifactValue is used only for fields whose container already
// declares tool/function/image/audio semantics. This lets a function name,
// image URL, or audio payload count even when its nested object has no type
// field, while leaving arbitrary metadata maps inert.
func retryFilterArtifactValue(value any, eventType string) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		for _, item := range v {
			if retryFilterArtifactValue(item, eventType) || retryFilterValueHasValidOutput(eventType, item) {
				return true
			}
		}
	case map[string]any:
		for _, key := range []string{"name", "id", "call_id", "arguments", "input", "partial_json", "result", "output", "action", "query", "queries", "url", "uri", "file_uri", "image_url", "source", "b64_json", "data", "audio", "transcript"} {
			if raw, exists := v[key]; exists && (retryFilterTextValue(raw, eventType) || retryFilterArtifactValue(raw, eventType)) {
				return true
			}
		}
	}
	return false
}
