package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// OpenAIGatewayHandler handles OpenAI API gateway requests
type OpenAIGatewayHandler struct {
	gatewayService      *service.OpenAIGatewayService
	billingCacheService *service.BillingCacheService
	concurrencyHelper   *ConcurrencyHelper
	maxAccountSwitches  int
}

// NewOpenAIGatewayHandler creates a new OpenAIGatewayHandler
func NewOpenAIGatewayHandler(
	gatewayService *service.OpenAIGatewayService,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	cfg *config.Config,
) *OpenAIGatewayHandler {
	pingInterval := time.Duration(0)
	maxAccountSwitches := 3
	if cfg != nil {
		pingInterval = time.Duration(cfg.Concurrency.PingInterval) * time.Second
		if cfg.Gateway.MaxAccountSwitches > 0 {
			maxAccountSwitches = cfg.Gateway.MaxAccountSwitches
		}
	}
	return &OpenAIGatewayHandler{
		gatewayService:      gatewayService,
		billingCacheService: billingCacheService,
		concurrencyHelper:   NewConcurrencyHelper(concurrencyService, SSEPingFormatComment, pingInterval),
		maxAccountSwitches:  maxAccountSwitches,
	}
}

// Responses handles OpenAI Responses API endpoint
// POST /openai/v1/responses
func (h *OpenAIGatewayHandler) Responses(c *gin.Context) {
	// Get apiKey and user from context (set by ApiKeyAuth middleware)
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}

	// Read request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false, body)

	// Parse request body to map for potential modification
	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	// Extract model and stream
	reqModel, _ := reqBody["model"].(string)
	reqStream, _ := reqBody["stream"].(bool)

	// 验证 model 必填
	if reqModel == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	userAgent := c.GetHeader("User-Agent")
	if !openai.IsCodexCLIRequest(userAgent) {
		existingInstructions, _ := reqBody["instructions"].(string)
		if strings.TrimSpace(existingInstructions) == "" {
			if instructions := strings.TrimSpace(service.GetOpenCodeInstructions()); instructions != "" {
				reqBody["instructions"] = instructions
				// Re-serialize body
				body, err = json.Marshal(reqBody)
				if err != nil {
					h.errorResponse(c, http.StatusInternalServerError, "api_error", "Failed to process request")
					return
				}
			}
		}
	}

	setOpsRequestContext(c, reqModel, reqStream, body)

	// 提前校验 function_call_output 是否具备可关联上下文，避免上游 400。
	// 要求 previous_response_id，或 input 内存在带 call_id 的 tool_call/function_call，
	// 或带 id 且与 call_id 匹配的 item_reference。
	if service.HasFunctionCallOutput(reqBody) {
		previousResponseID, _ := reqBody["previous_response_id"].(string)
		if strings.TrimSpace(previousResponseID) == "" && !service.HasToolCallContext(reqBody) {
			if service.HasFunctionCallOutputMissingCallID(reqBody) {
				log.Printf("[OpenAI Handler] function_call_output 缺少 call_id: model=%s", reqModel)
				h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "function_call_output requires call_id or previous_response_id; if relying on history, ensure store=true and reuse previous_response_id")
				return
			}
			callIDs := service.FunctionCallOutputCallIDs(reqBody)
			if !service.HasItemReferenceForCallIDs(reqBody, callIDs) {
				log.Printf("[OpenAI Handler] function_call_output 缺少匹配的 item_reference: model=%s", reqModel)
				h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "function_call_output requires item_reference ids matching each call_id, or previous_response_id/tool_call context; if relying on history, ensure store=true and reuse previous_response_id")
				return
			}
		}
	}

	// Track if we've started streaming (for error handling)
	streamStarted := false

	// Get subscription info (may be nil)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	// 初始化错误记录上下文
	errCtx := h.newOpenAIErrorRecordingContext(c, apiKey, subscription)
	errCtx.setModel(reqModel)
	errCtx.setStream(reqStream)

	// 0. Check if wait queue is full
	maxWait := service.CalculateMaxWait(subject.Concurrency)
	canWait, err := h.concurrencyHelper.IncrementWaitCount(c.Request.Context(), subject.UserID, maxWait)
	waitCounted := false
	if err != nil {
		log.Printf("Increment wait count failed: %v", err)
		// On error, allow request to proceed
	} else if !canWait {
		errCtx.recordError("concurrency_limit", http.StatusTooManyRequests, "Too many pending requests, please retry later", nil, "")
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later")
		return
	}
	if err == nil && canWait {
		waitCounted = true
	}
	defer func() {
		if waitCounted {
			h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		}
	}()

	// 1. First acquire user concurrency slot
	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted)
	if err != nil {
		log.Printf("User concurrency acquire failed: %v", err)
		errCtx.recordError("concurrency_limit", http.StatusTooManyRequests, "Concurrency limit exceeded for user, please retry later", nil, "")
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	// User slot acquired: no longer waiting.
	if waitCounted {
		h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		waitCounted = false
	}
	// 确保请求取消时也会释放槽位，避免长连接被动中断造成泄漏
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. Re-check billing eligibility after wait
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription); err != nil {
		log.Printf("Billing eligibility check failed after wait: %v", err)
		status, code, message := billingErrorDetails(err)
		errCtx.recordError("billing_error", status, message, nil, "")
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	// Generate session hash (header first; fallback to prompt_cache_key)
	sessionHash := h.gatewayService.GenerateSessionHash(c, reqBody)

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	failedAccountIDs := make(map[int64]struct{})
	lastFailoverStatus := 0

	for {
		// Select account supporting the requested model
		log.Printf("[OpenAI Handler] Selecting account: groupID=%v model=%s", apiKey.GroupID, reqModel)
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, sessionHash, reqModel, failedAccountIDs)
		if err != nil {
			log.Printf("[OpenAI Handler] SelectAccount failed: %v", err)
			if len(failedAccountIDs) == 0 {
				errCtx.recordError("no_account", http.StatusServiceUnavailable, "No available accounts: "+err.Error(), nil, "")
				h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts: "+err.Error(), streamStarted)
				return
			}
			status, _, errMsg := h.mapUpstreamError(lastFailoverStatus)
			errCtx.recordError("upstream_error", status, errMsg, &lastFailoverStatus, "")
			h.handleFailoverExhausted(c, lastFailoverStatus, streamStarted)
			return
		}
		account := selection.Account
		errCtx.setAccount(account)
		log.Printf("[OpenAI Handler] Selected account: id=%d name=%s", account.ID, account.Name)
		setOpsSelectedAccount(c, account.ID)

		// 3. Acquire account concurrency slot
		accountReleaseFunc := selection.ReleaseFunc
		if !selection.Acquired {
			if selection.WaitPlan == nil {
				errCtx.recordError("no_account", http.StatusServiceUnavailable, "No available accounts", nil, "")
				h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", streamStarted)
				return
			}
			accountWaitCounted := false
			canWait, err := h.concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
			if err != nil {
				log.Printf("Increment account wait count failed: %v", err)
			} else if !canWait {
				log.Printf("Account wait queue full: account=%d", account.ID)
				errCtx.recordError("concurrency_limit", http.StatusTooManyRequests, "Too many pending requests, please retry later", nil, "")
				h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later", streamStarted)
				return
			}
			if err == nil && canWait {
				accountWaitCounted = true
			}
			defer func() {
				if accountWaitCounted {
					h.concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
				}
			}()

			accountReleaseFunc, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
				c,
				account.ID,
				selection.WaitPlan.MaxConcurrency,
				selection.WaitPlan.Timeout,
				reqStream,
				&streamStarted,
			)
			if err != nil {
				log.Printf("Account concurrency acquire failed: %v", err)
				errCtx.recordError("concurrency_limit", http.StatusTooManyRequests, "Concurrency limit exceeded for account, please retry later", nil, "")
				h.handleConcurrencyError(c, err, "account", streamStarted)
				return
			}
			if accountWaitCounted {
				h.concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
				accountWaitCounted = false
			}
			if err := h.gatewayService.BindStickySession(c.Request.Context(), apiKey.GroupID, sessionHash, account.ID); err != nil {
				log.Printf("Bind sticky session failed: %v", err)
			}
		}
		// 账号槽位/等待计数需要在超时或断开时安全回收
		accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

		// Forward request
		result, err := h.gatewayService.Forward(c.Request.Context(), c, account, body)
		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}
		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				failedAccountIDs[account.ID] = struct{}{}
				if switchCount >= maxAccountSwitches {
					lastFailoverStatus = failoverErr.StatusCode
					status, _, errMsg := h.mapUpstreamError(lastFailoverStatus)
					errCtx.recordError("upstream_error", status, errMsg, &lastFailoverStatus, "")
					h.handleFailoverExhausted(c, lastFailoverStatus, streamStarted)
					return
				}
				lastFailoverStatus = failoverErr.StatusCode
				switchCount++
				log.Printf("Account %d: upstream error %d, switching account %d/%d", account.ID, failoverErr.StatusCode, switchCount, maxAccountSwitches)
				continue
			}
			// Error response already handled in Forward, record forward_error
			errCtx.recordError("forward_error", http.StatusBadGateway, err.Error(), nil, "")
			log.Printf("Account %d: Forward request failed: %v", account.ID, err)
			return
		}

		// 捕获请求信息（用于异步记录，避免在 goroutine 中访问 gin.Context）
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)

		// Async record usage
		go func(result *service.OpenAIForwardResult, usedAccount *service.Account, ua, ip string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
				Result:       result,
				APIKey:       apiKey,
				User:         apiKey.User,
				Account:      usedAccount,
				Subscription: subscription,
				UserAgent:    ua,
				IPAddress:    ip,
			}); err != nil {
				log.Printf("Record usage failed: %v", err)
			}
		}(result, account, userAgent, clientIP)
		return
	}
}

// handleConcurrencyError handles concurrency-related errors with proper 429 response
func (h *OpenAIGatewayHandler) handleConcurrencyError(c *gin.Context, err error, slotType string, streamStarted bool) {
	h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error",
		fmt.Sprintf("Concurrency limit exceeded for %s, please retry later", slotType), streamStarted)
}

func (h *OpenAIGatewayHandler) handleFailoverExhausted(c *gin.Context, statusCode int, streamStarted bool) {
	status, errType, errMsg := h.mapUpstreamError(statusCode)
	h.handleStreamingAwareError(c, status, errType, errMsg, streamStarted)
}

func (h *OpenAIGatewayHandler) mapUpstreamError(statusCode int) (int, string, string) {
	switch statusCode {
	case 401:
		return http.StatusBadGateway, "upstream_error", "Upstream authentication failed, please contact administrator"
	case 403:
		return http.StatusBadGateway, "upstream_error", "Upstream access forbidden, please contact administrator"
	case 429:
		return http.StatusTooManyRequests, "rate_limit_error", "Upstream rate limit exceeded, please retry later"
	case 529:
		return http.StatusServiceUnavailable, "upstream_error", "Upstream service overloaded, please retry later"
	case 500, 502, 503, 504:
		return http.StatusBadGateway, "upstream_error", "Upstream service temporarily unavailable"
	default:
		return http.StatusBadGateway, "upstream_error", "Upstream request failed"
	}
}

// handleStreamingAwareError handles errors that may occur after streaming has started
func (h *OpenAIGatewayHandler) handleStreamingAwareError(c *gin.Context, status int, errType, message string, streamStarted bool) {
	if streamStarted {
		// Stream already started, send error as SSE event then close
		flusher, ok := c.Writer.(http.Flusher)
		if ok {
			// Send error event in OpenAI SSE format
			errorEvent := fmt.Sprintf(`event: error`+"\n"+`data: {"error": {"type": "%s", "message": "%s"}}`+"\n\n", errType, message)
			if _, err := fmt.Fprint(c.Writer, errorEvent); err != nil {
				_ = c.Error(err)
			}
			flusher.Flush()
		}
		return
	}

	// Normal case: return JSON response with proper status code
	h.errorResponse(c, status, errType, message)
}

// errorResponse returns OpenAI API format error response
func (h *OpenAIGatewayHandler) errorResponse(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// openaiErrorRecordingContext 用于跟踪请求上下文以便记录错误
type openaiErrorRecordingContext struct {
	handler      *OpenAIGatewayHandler
	c            *gin.Context
	startTime    time.Time
	requestID    string
	apiKey       *service.APIKey
	subscription *service.UserSubscription
	model        string
	stream       bool
	account      *service.Account
}

// newOpenAIErrorRecordingContext 创建新的错误记录上下文
func (h *OpenAIGatewayHandler) newOpenAIErrorRecordingContext(c *gin.Context, apiKey *service.APIKey, subscription *service.UserSubscription) *openaiErrorRecordingContext {
	return &openaiErrorRecordingContext{
		handler:      h,
		c:            c,
		startTime:    time.Now(),
		requestID:    "err-" + uuid.New().String(),
		apiKey:       apiKey,
		subscription: subscription,
	}
}

// setModel 设置请求的模型
func (e *openaiErrorRecordingContext) setModel(model string) {
	e.model = model
}

// setStream 设置是否为流式请求
func (e *openaiErrorRecordingContext) setStream(stream bool) {
	e.stream = stream
}

// setAccount 设置选中的账号
func (e *openaiErrorRecordingContext) setAccount(account *service.Account) {
	e.account = account
}

// recordError 异步记录错误到使用日志
func (e *openaiErrorRecordingContext) recordError(errorType string, statusCode int, message string, upstreamStatusCode *int, upstreamErrorMessage string) {
	if e.apiKey == nil || e.apiKey.User == nil {
		return
	}

	userAgent := e.c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(e.c)
	durationMs := int(time.Since(e.startTime).Milliseconds())

	// 收集请求头（白名单过滤）
	requestHeaders := make(map[string]string)
	headerWhitelist := []string{
		"Content-Type",
		"Accept",
		"X-Request-ID",
		"X-Forwarded-For",
		"X-Real-IP",
		"Authorization",
		"OpenAI-Beta",
	}
	for _, header := range headerWhitelist {
		if value := e.c.GetHeader(header); value != "" {
			// 对敏感头部进行脱敏
			if header == "Authorization" {
				if len(value) > 10 {
					value = value[:10] + "..."
				}
			}
			requestHeaders[header] = value
		}
	}

	// 构建错误响应体
	errorBody, _ := json.Marshal(map[string]any{
		"error": map[string]string{
			"type":    errorType,
			"message": message,
		},
	})

	input := &service.OpenAIRecordErrorUsageInput{
		RequestID:            e.requestID,
		APIKey:               e.apiKey,
		User:                 e.apiKey.User,
		Account:              e.account,
		Subscription:         e.subscription,
		Model:                e.model,
		Stream:               e.stream,
		UserAgent:            userAgent,
		IPAddress:            clientIP,
		RequestHeaders:       requestHeaders,
		ErrorType:            errorType,
		ErrorStatusCode:      statusCode,
		ErrorMessage:         message,
		ErrorBody:            string(errorBody),
		UpstreamStatusCode:   upstreamStatusCode,
		UpstreamErrorMessage: upstreamErrorMessage,
		DurationMs:           durationMs,
	}

	// 异步记录
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := e.handler.gatewayService.RecordErrorUsage(ctx, input); err != nil {
			log.Printf("Record error usage failed: %v", err)
		}
	}()
}
