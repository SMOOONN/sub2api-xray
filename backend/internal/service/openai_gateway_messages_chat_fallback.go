package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// forwardAnthropicViaRawChatCompletions serves /v1/messages clients through
// an OpenAI-compatible upstream that only supports /v1/chat/completions.
//
// Conversion chain (direct, no Responses intermediary):
//
//	Request:  Anthropic Messages → Chat Completions (AnthropicToChatCompletionsRequest)
//	Response: CC chunk/response → Anthropic events/response (direct bridge)
//
// This is the /v1/messages counterpart of forwardResponsesViaRawChatCompletions
// (which serves /v1/responses clients). Unlike the Responses path, the direct
// bridge skips the Responses API intermediate representation entirely — every
// streaming token runs through a single state machine instead of two.
func (s *OpenAIGatewayService) forwardAnthropicViaRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	// 1. Parse Anthropic request
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse anthropic request: %w", err)
	}
	originalModel := anthropicReq.Model
	if strings.TrimSpace(originalModel) == "" {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	applyOpenAICompatModelNormalization(&anthropicReq)
	clientStream := anthropicReq.Stream

	// 2. Anthropic → Chat Completions (direct, no Responses intermediary)
	chatReq, err := apicompat.AnthropicToChatCompletionsRequestWithMinOutputTokens(
		&anthropicReq,
		configuredMinOutputTokens(s.cfg),
	)
	if err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert anthropic to chat completions: %w", err)
	}

	billingModel := resolveOpenAIForwardModel(account, anthropicReq.Model, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	if err := validateGPT61SolCompatRequest(body, upstreamModel); err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	if openai.RequiresResponsesForToolCalls(upstreamModel) && len(anthropicReq.Tools) > 0 {
		err := fmt.Errorf("%s requires Responses for tool calls; this account only supports Chat Completions", normalizeKnownOpenAICodexModel(upstreamModel))
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	chatReq.Model = upstreamModel
	chatReq.ReasoningEffort = openAICompatAnthropicReasoningEffort(&anthropicReq, upstreamModel, chatReq.ReasoningEffort)
	chatReq.Stream = clientStream
	if clientStream {
		chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}

	serviceTier := extractOpenAIServiceTierFromBody(body)

	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions request: %w", err)
	}
	if normalizedBody, normalized := NormalizeGLMOpenAIReasoningEffort(chatBody, upstreamModel); normalized {
		chatBody = normalizedBody
	}
	if account.Platform == PlatformOpenAI {
		policyBody, changed, policyErr := ApplyOpenAIReasoningEffortPolicyFromContext(ctx, chatBody)
		if policyErr != nil {
			if IsReasoningEffortPolicyDenied(policyErr) {
				MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
				writeAnthropicError(c, http.StatusForbidden, "forbidden_error", policyErr.Error())
			}
			return nil, policyErr
		}
		if changed {
			chatBody = policyBody
		}
	}
	// Provider normalization and policy caps can both change the converted effort.
	// Use the final outbound value for usage logs and billing.
	reasoningEffort := extractOpenAIReasoningEffortFromBody(chatBody, upstreamModel, billingModel, originalModel)
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, chatBody, upstreamModel)
	// Unlike forwardResponsesViaRawChatCompletions, applyOpenAIFastPolicyToBody
	// is intentionally skipped: Anthropic Messages bodies carry no service_tier,
	// so the converted Chat Completions body never contains one and the policy
	// would always be a no-op on this path.

	logger.L().Debug("openai messages: forwarding via raw chat completions",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
	)

	// 3. Build and send upstream request via the shared CC pipeline
	apiKey, targetURL, err := s.resolveCCFallbackTarget(account)
	if err != nil {
		return nil, err
	}
	resp, err := s.sendCCUpstreamRequest(ctx, c, account, targetURL, chatBody, clientStream, apiKey, account.GetOpenAIUserAgent(), "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// 4. Handle error responses
	if resp.StatusCode >= 400 {
		respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
		foErr, candidate := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel)
		if foErr != nil {
			return nil, foErr
		}
		// Non-failover error: return Anthropic-formatted error to client via the
		// shared compat handler (passthrough rules, ops recording, cyber_policy).
		return s.handleCompatErrorResponseWithDecision(resp, c, account, writeAnthropicError, candidate, billingModel)
	}

	// 5. Convert response
	if clientStream {
		return s.streamChatCompletionsAsAnthropic(c, account, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	return s.bufferChatCompletionsAsAnthropic(c, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
}

func (s *OpenAIGatewayService) bufferChatCompletionsAsAnthropic(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	ccResp, usage, err := s.readCCUpstreamJSONResponse(c, resp, writeAnthropicError)
	if err != nil {
		return nil, err
	}
	anthropicResp := apicompat.ChatCompletionsResponseToAnthropic(ccResp, originalModel)

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.JSON(http.StatusOK, anthropicResp)

	return &OpenAIForwardResult{
		RequestID:                   requestID,
		UpstreamHeaders:             resp.Header,
		Usage:                       usage,
		Model:                       originalModel,
		BillingModel:                billingModel,
		UpstreamModel:               upstreamModel,
		ReasoningEffort:             reasoningEffort,
		UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
		ServiceTier:                 resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                      false,
		Duration:                    time.Since(startTime),
	}, nil
}

func (s *OpenAIGatewayService) streamChatCompletionsAsAnthropic(
	c *gin.Context,
	account *Account,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	ctx := c.Request.Context()
	requestID := resp.Header.Get("x-request-id")
	writeStreamHeaders := s.newStreamHeaderWriter(c, resp.Header)
	anthropicState := apicompat.NewChatCompletionsToAnthropicStreamState(originalModel)
	clientDisconnected := false
	var stage *openAIFirstOutputStage
	stopKeepalive := func() {}
	if retryFilterShouldStage(ctx, account) {
		stage = newDefaultOpenAIFirstOutputStage()
		defer func() {
			if err := stage.Close(); err != nil {
				logger.L().Warn("openai messages: close retry-filter stage", zap.Error(err))
			}
		}()
		interval := 0
		if s.cfg != nil {
			interval = s.cfg.Gateway.StreamKeepaliveInterval
		}
		stopKeepalive = startOpenAISSEKeepalive(c, retryFilterKeepaliveInterval(interval))
	}
	defer func() { stopKeepalive() }()
	staging := stage != nil
	semanticOutputSeen := false
	commitStage := func() error {
		stopKeepalive()
		if !staging {
			return nil
		}
		staging = false
		if stage.Buffered() == 0 {
			return stage.Close()
		}
		writeStreamHeaders()
		if err := stage.CommitTo(c.Writer); err != nil {
			clientDisconnected = true
			return err
		}
		c.Writer.Flush()
		return nil
	}
	writeEvents := func(events []apicompat.AnthropicStreamEvent) error {
		if clientDisconnected {
			return nil
		}
		for _, evt := range events {
			sse, err := apicompat.ResponsesAnthropicEventToSSE(evt)
			if err != nil {
				continue
			}
			if staging {
				if _, err := stage.WriteString(sse); err != nil {
					return err
				}
				continue
			}
			stopKeepalive()
			writeStreamHeaders()
			if _, err := fmt.Fprint(c.Writer, sse); err != nil {
				clientDisconnected = true
				break
			}
		}
		if !staging && !clientDisconnected && len(events) > 0 {
			c.Writer.Flush()
		}
		return nil
	}
	inspect := func(payload []byte) error {
		if !semanticOutputSeen && (gjson.GetBytes(payload, "error").Exists() || gjson.GetBytes(payload, "type").String() == "error") {
			message := extractOpenAISSEErrorMessage(payload)
			candidate := s.openAIStreamFailureCandidate(account, payload, message, resp.Header)
			candidate.NextAccountAction = NextAccountStop
			decision := prepareRetryFilterFailure(ctx, c, account, candidate)
			if decision != nil {
				recordRetryFilterFailure(c, account, candidate)
			}
			if decision.Actionable() {
				return candidate
			}
		}
		if RetryFilterResponseHasValidOutput("", payload) {
			semanticOutputSeen = true
			if staging {
				return commitStage()
			}
		}
		return nil
	}
	scan := s.scanCCStream(c, resp, "openai messages chat fallback", requestID, startTime,
		func(chunk *apicompat.ChatCompletionsChunk) error {
			return writeEvents(apicompat.ChatCompletionsChunkToAnthropicEvents(chunk, anthropicState))
		}, inspect)
	result := func() *OpenAIForwardResult {
		return &OpenAIForwardResult{
			RequestID: requestID, UpstreamHeaders: resp.Header, Usage: scan.Usage,
			Model: originalModel, BillingModel: billingModel, UpstreamModel: upstreamModel,
			ReasoningEffort: reasoningEffort, ServiceTier: resolvedOpenAIUpstreamServiceTier(c, serviceTier),
			UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
			Stream:                      true, Duration: time.Since(startTime), FirstTokenMs: scan.FirstTokenMs,
			ClientDisconnect: clientDisconnected,
		}
	}
	if scan.CallbackErr != nil {
		return result(), scan.CallbackErr
	}
	if ctx.Err() != nil || errors.Is(scan.Err, context.Canceled) || errors.Is(scan.Err, context.DeadlineExceeded) {
		if scan.Err == nil {
			scan.Err = ctx.Err()
		}
		return result(), fmt.Errorf("stream usage incomplete: %w", scan.Err)
	}
	// Scanner/staging budgets are gateway resource failures, not evidence of
	// an empty upstream response. They must not consume a retry-filter budget.
	if errors.Is(scan.Err, bufio.ErrTooLong) || errors.Is(scan.Err, errOpenAIFirstOutputScannerLimit) {
		return result(), scan.Err
	}
	if !scan.SemanticOutputSeen && len(scan.ErrorPayload) == 0 {
		eventType := RetryFilterEventEmptyInterruptedStream
		if scan.Err == nil && (scan.SawDone || scan.SawFinish) {
			eventType = RetryFilterEventEmptyCompletedResponse
		}
		candidate := newRetryFilterStreamCandidate(resp.Header, nil, eventType, "upstream stream ended before valid output")
		decision := prepareRetryFilterFailure(ctx, c, account, candidate)
		if decision != nil {
			recordRetryFilterFailure(c, account, candidate)
		}
		if decision.Actionable() {
			return result(), candidate
		}
	}
	if err := commitStage(); err != nil {
		return result(), err
	}
	if scan.Err != nil {
		return result(), fmt.Errorf("stream usage incomplete: %w", scan.Err)
	}
	// No actionable match retains the legacy converter's completion behavior.
	if err := writeEvents(apicompat.FinalizeChatCompletionsAnthropicStream(anthropicState)); err != nil {
		return result(), err
	}
	if !scan.SawDone {
		logCCStreamMissingDoneSentinel("openai messages chat fallback", requestID)
	}
	return result(), nil
}
