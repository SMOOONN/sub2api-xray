package model

import (
	"fmt"
	"strings"
	"time"
)

const (
	RetryFilterActionSameAccountRetry = "same_account_retry"
	RetryFilterActionSwitchAccount    = "switch_account"
	RetryFilterActionTerminate        = "terminate"
	RetryFilterActionObserve          = "observe"
)

const (
	RetryFilterEmptyRequestNone       = "none"
	RetryFilterEmptyRequestBody       = "body"
	RetryFilterEmptyRequestStructured = "structured"
	RetryFilterEmptyRequestBoth       = "both"

	RetryFilterEmptyResponseNone        = "none"
	RetryFilterEmptyResponseCompleted   = "completed"
	RetryFilterEmptyResponseInterrupted = "interrupted"
	RetryFilterEmptyResponseBoth        = "both"
)

var DefaultRetryFilterUpstreamCodePaths = []string{"error.type", "error.code", "code", "type"}

type RetryFilterRule struct {
	ID                  int64     `json:"id"`
	PresetID            int64     `json:"preset_id"`
	Name                string    `json:"name"`
	Enabled             bool      `json:"enabled"`
	Priority            int       `json:"priority"`
	StatusCodes         []int     `json:"status_codes"`
	Keywords            []string  `json:"keywords"`
	UpstreamCodes       []string  `json:"upstream_codes"`
	UpstreamCodePaths   []string  `json:"upstream_code_paths"`
	Models              []string  `json:"models"`
	MatchMode           string    `json:"match_mode"`
	Action              string    `json:"action"`
	RetryCountOverride  *int      `json:"retry_count_override,omitempty"`
	RetryDelayMs        *int      `json:"retry_delay_ms,omitempty"`
	EmptyRequestMode    string    `json:"empty_request_mode,omitempty"`
	EmptyResponseMode   string    `json:"empty_response_mode,omitempty"`
	TerminateStatusCode *int      `json:"terminate_status_code,omitempty"`
	TerminateMessage    *string   `json:"terminate_message,omitempty"`
	Description         *string   `json:"description,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (r *RetryFilterRule) Validate() error {
	if r == nil {
		return &ValidationError{Field: "rule", Message: "rule is required"}
	}
	if r.Name == "" {
		return &ValidationError{Field: "name", Message: "name is required"}
	}
	if r.MatchMode == "" {
		r.MatchMode = MatchModeAny
	}
	r.EmptyRequestMode = strings.ToLower(strings.TrimSpace(r.EmptyRequestMode))
	r.EmptyResponseMode = strings.ToLower(strings.TrimSpace(r.EmptyResponseMode))
	if r.MatchMode != MatchModeAny && r.MatchMode != MatchModeAll {
		return &ValidationError{Field: "match_mode", Message: "match_mode must be 'any' or 'all'"}
	}
	emptyRequestConfigured := r.EmptyRequestMode != "" && r.EmptyRequestMode != RetryFilterEmptyRequestNone
	emptyResponseConfigured := r.EmptyResponseMode != "" && r.EmptyResponseMode != RetryFilterEmptyResponseNone
	if len(r.StatusCodes) == 0 && len(r.Keywords) == 0 && len(r.UpstreamCodes) == 0 && len(r.Models) == 0 && !emptyRequestConfigured && !emptyResponseConfigured {
		return &ValidationError{Field: "conditions", Message: "at least one condition is required"}
	}
	for i, keyword := range r.Keywords {
		r.Keywords[i] = strings.TrimSpace(keyword)
		if r.Keywords[i] == "" {
			return &ValidationError{Field: "keywords", Message: "keywords must not be empty"}
		}
	}
	for i, code := range r.UpstreamCodes {
		r.UpstreamCodes[i] = strings.TrimSpace(code)
		if r.UpstreamCodes[i] == "" {
			return &ValidationError{Field: "upstream_codes", Message: "upstream codes must not be empty"}
		}
	}
	for _, code := range r.StatusCodes {
		if code < 100 || code > 599 {
			return &ValidationError{Field: "status_codes", Message: "status codes must be between 100 and 599"}
		}
	}
	for i, modelName := range r.Models {
		r.Models[i] = strings.TrimSpace(modelName)
		modelName = r.Models[i]
		if modelName == "" {
			return &ValidationError{Field: "models", Message: "model names must not be empty"}
		}
		if strings.Contains(modelName, "*") && (modelName == "*" || !strings.HasSuffix(modelName, "*") || strings.Contains(modelName[:len(modelName)-1], "*")) {
			return &ValidationError{Field: "models", Message: "model wildcard is allowed only as a non-empty trailing suffix"}
		}
	}
	for i, path := range r.UpstreamCodePaths {
		r.UpstreamCodePaths[i] = strings.TrimSpace(path)
		if r.UpstreamCodePaths[i] == "" {
			return &ValidationError{Field: "upstream_code_paths", Message: "upstream code paths must not be empty"}
		}
	}
	if r.Action == "" {
		r.Action = RetryFilterActionSameAccountRetry
	}
	if r.EmptyRequestMode == "" {
		r.EmptyRequestMode = RetryFilterEmptyRequestNone
	}
	if r.EmptyResponseMode == "" {
		r.EmptyResponseMode = RetryFilterEmptyResponseNone
	}
	switch r.EmptyRequestMode {
	case RetryFilterEmptyRequestNone, RetryFilterEmptyRequestBody, RetryFilterEmptyRequestStructured, RetryFilterEmptyRequestBoth:
	default:
		return &ValidationError{Field: "empty_request_mode", Message: "empty_request_mode must be none, body, structured, or both"}
	}
	switch r.EmptyResponseMode {
	case RetryFilterEmptyResponseNone, RetryFilterEmptyResponseCompleted, RetryFilterEmptyResponseInterrupted, RetryFilterEmptyResponseBoth:
	default:
		return &ValidationError{Field: "empty_response_mode", Message: "empty_response_mode must be none, completed, interrupted, or both"}
	}
	if r.RetryDelayMs != nil && (*r.RetryDelayMs < 0 || *r.RetryDelayMs > 60000) {
		return &ValidationError{Field: "retry_delay_ms", Message: "retry_delay_ms must be between 0 and 60000"}
	}
	switch r.Action {
	case RetryFilterActionSameAccountRetry:
		if r.RetryCountOverride != nil && (*r.RetryCountOverride < 0 || *r.RetryCountOverride > 50) {
			return &ValidationError{Field: "retry_count_override", Message: "retry_count_override must be between 0 and 50"}
		}
	case RetryFilterActionSwitchAccount, RetryFilterActionObserve:
		if r.RetryCountOverride != nil {
			return &ValidationError{Field: "retry_count_override", Message: fmt.Sprintf("retry_count_override is only valid for %s", RetryFilterActionSameAccountRetry)}
		}
		if r.RetryDelayMs != nil {
			return &ValidationError{Field: "retry_delay_ms", Message: fmt.Sprintf("retry_delay_ms is only valid for %s", RetryFilterActionSameAccountRetry)}
		}
	case RetryFilterActionTerminate:
		if r.RetryCountOverride != nil {
			return &ValidationError{Field: "retry_count_override", Message: fmt.Sprintf("retry_count_override is only valid for %s", RetryFilterActionSameAccountRetry)}
		}
		if r.RetryDelayMs != nil {
			return &ValidationError{Field: "retry_delay_ms", Message: fmt.Sprintf("retry_delay_ms is only valid for %s", RetryFilterActionSameAccountRetry)}
		}
		if r.TerminateStatusCode != nil && (*r.TerminateStatusCode < 100 || *r.TerminateStatusCode > 599) {
			return &ValidationError{Field: "terminate_status_code", Message: "terminate status code must be between 100 and 599"}
		}
	default:
		return &ValidationError{Field: "action", Message: "action must be same_account_retry, switch_account, terminate, or observe"}
	}
	if r.Action != RetryFilterActionTerminate && (r.TerminateStatusCode != nil || r.TerminateMessage != nil) {
		return &ValidationError{Field: "terminate", Message: "terminate parameters are only valid for terminate action"}
	}
	if len(r.UpstreamCodePaths) == 0 {
		r.UpstreamCodePaths = append([]string(nil), DefaultRetryFilterUpstreamCodePaths...)
	}
	return nil
}
