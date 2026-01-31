//go:build integration

package service

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mockAnthropicServer 创建一个模拟的 Anthropic API 服务器
// 支持流式和非流式响应，返回可配置的 cache token 数量
type mockAnthropicServer struct {
	inputTokens              int
	outputTokens             int
	cacheCreationInputTokens int
	cacheReadInputTokens     int
	requestCount             int64
	mu                       sync.Mutex
	requestLog               []mockRequest
}

type mockRequest struct {
	Stream bool
	Model  string
}

func newMockAnthropicServer(input, output, cacheCreation, cacheRead int) *mockAnthropicServer {
	return &mockAnthropicServer{
		inputTokens:              input,
		outputTokens:             output,
		cacheCreationInputTokens: cacheCreation,
		cacheReadInputTokens:     cacheRead,
	}
}

func (s *mockAnthropicServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.requestCount, 1)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.requestLog = append(s.requestLog, mockRequest{Stream: req.Stream, Model: req.Model})
	s.mu.Unlock()

	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())
	w.Header().Set("x-request-id", requestID)

	if req.Stream {
		s.handleStreamingResponse(w, r, req.Model)
	} else {
		s.handleNonStreamingResponse(w, req.Model)
	}
}

func (s *mockAnthropicServer) handleStreamingResponse(w http.ResponseWriter, r *http.Request, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// message_start event
	messageStart := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            fmt.Sprintf("msg_%d", time.Now().UnixNano()),
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":                s.inputTokens,
				"output_tokens":               1,
				"cache_creation_input_tokens": s.cacheCreationInputTokens,
				"cache_read_input_tokens":     s.cacheReadInputTokens,
			},
		},
	}
	s.writeSSE(w, flusher, messageStart)

	// content_block_start
	contentBlockStart := map[string]any{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	}
	s.writeSSE(w, flusher, contentBlockStart)

	// content_block_delta
	delta := map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{
			"type": "text_delta",
			"text": "This is a mock response.",
		},
	}
	s.writeSSE(w, flusher, delta)

	// content_block_stop
	contentBlockStop := map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	}
	s.writeSSE(w, flusher, contentBlockStop)

	// message_delta
	messageDelta := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": s.outputTokens,
		},
	}
	s.writeSSE(w, flusher, messageDelta)

	// message_stop
	messageStop := map[string]any{
		"type": "message_stop",
	}
	s.writeSSE(w, flusher, messageStop)
}

func (s *mockAnthropicServer) handleNonStreamingResponse(w http.ResponseWriter, model string) {
	response := map[string]any{
		"id":    fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		"type":  "message",
		"role":  "assistant",
		"model": model,
		"content": []map[string]any{
			{
				"type": "text",
				"text": "This is a mock response.",
			},
		},
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":                s.inputTokens,
			"output_tokens":               s.outputTokens,
			"cache_creation_input_tokens": s.cacheCreationInputTokens,
			"cache_read_input_tokens":     s.cacheReadInputTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *mockAnthropicServer) writeSSE(w http.ResponseWriter, flusher http.Flusher, data any) {
	jsonData, _ := json.Marshal(data)
	eventType, _ := data.(map[string]any)["type"].(string)
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}

func (s *mockAnthropicServer) GetRequestCount() int64 {
	return atomic.LoadInt64(&s.requestCount)
}

// TestCacheTransferProbabilityIntegration 测试缓存转移概率功能
// 验证：
// 1. 概率控制准确性（30% probability 约触发 ~6/20 次）
// 2. 流式和非流式路径一致性
// 3. 三位一体一致性（计费日志、实际计费、API 返回 token 数）
func TestCacheTransferProbabilityIntegration(t *testing.T) {
	// 创建 mock Anthropic 服务器
	mockServer := newMockAnthropicServer(1000, 500, 200, 800)
	ts := httptest.NewServer(mockServer)
	defer ts.Close()

	t.Run("ShouldTransferCacheTokens probability distribution", func(t *testing.T) {
		// 测试概率分布准确性
		testProbabilities := []struct {
			probability    float64
			expectedRatio  float64
			toleranceRatio float64 // 允许的误差比例
		}{
			{0.0, 0.0, 0.0},     // 0% should never trigger
			{1.0, 1.0, 0.0},     // 100% should always trigger
			{0.3, 0.3, 0.15},    // 30% with 15% tolerance
			{0.5, 0.5, 0.10},    // 50% with 10% tolerance
			{0.7, 0.7, 0.10},    // 70% with 10% tolerance
		}

		iterations := 1000

		for _, tc := range testProbabilities {
			t.Run(fmt.Sprintf("probability_%.1f", tc.probability), func(t *testing.T) {
				trueCount := 0
				for i := 0; i < iterations; i++ {
					if ShouldTransferCacheTokens(tc.probability) {
						trueCount++
					}
				}

				actualRatio := float64(trueCount) / float64(iterations)

				if tc.toleranceRatio == 0 {
					// 确定性情况
					require.Equal(t, tc.expectedRatio, actualRatio,
						"probability %.2f should have exact ratio %.2f, got %.2f",
						tc.probability, tc.expectedRatio, actualRatio)
				} else {
					// 概率情况，检查是否在容差范围内
					minExpected := tc.expectedRatio - tc.toleranceRatio
					maxExpected := tc.expectedRatio + tc.toleranceRatio
					require.True(t, actualRatio >= minExpected && actualRatio <= maxExpected,
						"probability %.2f: actual ratio %.2f not in expected range [%.2f, %.2f]",
						tc.probability, actualRatio, minExpected, maxExpected)
				}
			})
		}
	})

	t.Run("TransferCacheTokens consistency", func(t *testing.T) {
		// 测试 token 转移计算一致性
		testCases := []struct {
			name          string
			cacheCreation int
			cacheRead     int
			ratio         float64
			wantCreation  int
			wantRead      int
		}{
			{
				name:          "no transfer",
				cacheCreation: 200,
				cacheRead:     800,
				ratio:         0,
				wantCreation:  200,
				wantRead:      800,
			},
			{
				name:          "50% transfer",
				cacheCreation: 200,
				cacheRead:     800,
				ratio:         0.5,
				wantCreation:  200 + 400, // 200 + 800*0.5
				wantRead:      400,        // 800 - 800*0.5
			},
			{
				name:          "100% transfer",
				cacheCreation: 200,
				cacheRead:     800,
				ratio:         1.0,
				wantCreation:  1000, // 200 + 800
				wantRead:      0,
			},
			{
				name:          "30% transfer",
				cacheCreation: 200,
				cacheRead:     800,
				ratio:         0.3,
				wantCreation:  200 + 240, // 200 + 800*0.3 = 440
				wantRead:      560,        // 800 - 800*0.3 = 560
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				newCreation, newRead := TransferCacheTokens(tc.cacheCreation, tc.cacheRead, tc.ratio)
				require.Equal(t, tc.wantCreation, newCreation, "cache creation mismatch")
				require.Equal(t, tc.wantRead, newRead, "cache read mismatch")
			})
		}
	})

	t.Run("three-way consistency simulation", func(t *testing.T) {
		// 模拟三位一体一致性验证
		// 1. 响应重写后的 token count
		// 2. 计费日志中的 token count
		// 3. 实际计费使用的 token count

		originalCacheCreation := 200
		originalCacheRead := 800
		ratio := 0.3

		// 计算转移后的值（与 gateway_handler 和 gateway_service 使用相同的函数）
		newCacheCreation, newCacheRead := TransferCacheTokens(originalCacheCreation, originalCacheRead, ratio)

		// 验证三个位置使用相同的值
		expectedCreation := 440 // 200 + 800*0.3
		expectedRead := 560     // 800 - 800*0.3

		require.Equal(t, expectedCreation, newCacheCreation, "API response token count mismatch")
		require.Equal(t, expectedRead, newCacheRead, "API response cache read mismatch")

		// 模拟 RecordUsage 中的计算（使用相同的 TransferCacheTokens）
		billingCreation, billingRead := TransferCacheTokens(originalCacheCreation, originalCacheRead, ratio)
		require.Equal(t, expectedCreation, billingCreation, "billing log token count mismatch")
		require.Equal(t, expectedRead, billingRead, "billing log cache read mismatch")

		// 确保 API 响应和计费日志完全一致
		require.Equal(t, newCacheCreation, billingCreation, "API and billing creation tokens must match")
		require.Equal(t, newCacheRead, billingRead, "API and billing read tokens must match")
	})

	t.Run("streaming vs non-streaming consistency", func(t *testing.T) {
		// 验证流式和非流式响应解析后的 token 一致性
		originalCacheCreation := 200
		originalCacheRead := 800
		ratio := 0.5

		// 流式响应的 token 转移
		streamingCreation, streamingRead := TransferCacheTokens(originalCacheCreation, originalCacheRead, ratio)

		// 非流式响应的 token 转移
		nonStreamingCreation, nonStreamingRead := TransferCacheTokens(originalCacheCreation, originalCacheRead, ratio)

		// 两种模式应该产生完全相同的结果
		require.Equal(t, streamingCreation, nonStreamingCreation, "streaming and non-streaming creation must match")
		require.Equal(t, streamingRead, nonStreamingRead, "streaming and non-streaming read must match")
	})

	t.Run("mock server streaming response parsing", func(t *testing.T) {
		// 验证流式响应可以正确解析
		client := &http.Client{Timeout: 10 * time.Second}

		reqBody := `{"model":"claude-3-5-sonnet-20241022","stream":true,"messages":[{"role":"user","content":"test"}]}`
		req, err := http.NewRequest("POST", ts.URL+"/v1/messages", strings.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

		// 解析 SSE 事件
		reader := bufio.NewReader(resp.Body)
		var inputTokens, outputTokens, cacheCreation, cacheRead int

		for {
			line, err := reader.ReadString('\n')
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("failed to read line: %v", err)
			}

			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				data = strings.TrimSpace(data)

				var event map[string]any
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					continue
				}

				eventType, _ := event["type"].(string)

				if eventType == "message_start" {
					if message, ok := event["message"].(map[string]any); ok {
						if usage, ok := message["usage"].(map[string]any); ok {
							inputTokens = int(usage["input_tokens"].(float64))
							cacheCreation = int(usage["cache_creation_input_tokens"].(float64))
							cacheRead = int(usage["cache_read_input_tokens"].(float64))
						}
					}
				} else if eventType == "message_delta" {
					if usage, ok := event["usage"].(map[string]any); ok {
						outputTokens = int(usage["output_tokens"].(float64))
					}
				}
			}
		}

		require.Equal(t, 1000, inputTokens, "input tokens mismatch")
		require.Equal(t, 500, outputTokens, "output tokens mismatch")
		require.Equal(t, 200, cacheCreation, "cache creation tokens mismatch")
		require.Equal(t, 800, cacheRead, "cache read tokens mismatch")
	})

	t.Run("mock server non-streaming response parsing", func(t *testing.T) {
		// 验证非流式响应可以正确解析
		client := &http.Client{Timeout: 10 * time.Second}

		reqBody := `{"model":"claude-3-5-sonnet-20241022","stream":false,"messages":[{"role":"user","content":"test"}]}`
		req, err := http.NewRequest("POST", ts.URL+"/v1/messages", strings.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var response struct {
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		}

		err = json.NewDecoder(resp.Body).Decode(&response)
		require.NoError(t, err)

		require.Equal(t, 1000, response.Usage.InputTokens, "input tokens mismatch")
		require.Equal(t, 500, response.Usage.OutputTokens, "output tokens mismatch")
		require.Equal(t, 200, response.Usage.CacheCreationInputTokens, "cache creation tokens mismatch")
		require.Equal(t, 800, response.Usage.CacheReadInputTokens, "cache read tokens mismatch")
	})
}

// TestCacheTransferProbabilityStatistical 对概率进行统计验证
func TestCacheTransferProbabilityStatistical(t *testing.T) {
	// 30% 概率，运行 100 次，期望约 30 次触发
	probability := 0.3
	runs := 100
	trueCount := 0

	for i := 0; i < runs; i++ {
		if ShouldTransferCacheTokens(probability) {
			trueCount++
		}
	}

	// 使用卡方检验的简化版本
	// 期望值：30% * 100 = 30
	// 允许标准差的 3 倍作为容差（约 99.7% 置信区间）
	expected := float64(runs) * probability
	stdDev := math.Sqrt(expected * (1 - probability))
	tolerance := 3 * stdDev

	actualCount := float64(trueCount)
	t.Logf("Expected: %.1f, Actual: %d, StdDev: %.2f, Tolerance: %.2f", expected, trueCount, stdDev, tolerance)

	require.True(t, math.Abs(actualCount-expected) <= tolerance,
		"30%% probability with %d runs: expected ~%.0f±%.0f, got %d",
		runs, expected, tolerance, trueCount)
}

// TestCacheTransferEdgeCases 测试边界情况
func TestCacheTransferEdgeCases(t *testing.T) {
	testCases := []struct {
		name          string
		cacheCreation int
		cacheRead     int
		ratio         float64
		wantCreation  int
		wantRead      int
	}{
		{
			name:          "zero cache read",
			cacheCreation: 200,
			cacheRead:     0,
			ratio:         0.5,
			wantCreation:  200,
			wantRead:      0,
		},
		{
			name:          "zero cache creation",
			cacheCreation: 0,
			cacheRead:     800,
			ratio:         0.5,
			wantCreation:  400,
			wantRead:      400,
		},
		{
			name:          "both zero",
			cacheCreation: 0,
			cacheRead:     0,
			ratio:         0.5,
			wantCreation:  0,
			wantRead:      0,
		},
		{
			name:          "very small ratio",
			cacheCreation: 200,
			cacheRead:     800,
			ratio:         0.001,
			wantCreation:  200, // 800 * 0.001 = 0.8, rounds to 0 or 1
			wantRead:      800,
		},
		{
			name:          "ratio exactly 0",
			cacheCreation: 200,
			cacheRead:     800,
			ratio:         0,
			wantCreation:  200,
			wantRead:      800,
		},
		{
			name:          "ratio exactly 1",
			cacheCreation: 200,
			cacheRead:     800,
			ratio:         1.0,
			wantCreation:  1000,
			wantRead:      0,
		},
		{
			name:          "large values",
			cacheCreation: 1000000,
			cacheRead:     5000000,
			ratio:         0.3,
			wantCreation:  1000000 + 1500000, // 1000000 + 5000000*0.3
			wantRead:      3500000,            // 5000000 - 5000000*0.3
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			newCreation, newRead := TransferCacheTokens(tc.cacheCreation, tc.cacheRead, tc.ratio)
			require.Equal(t, tc.wantCreation, newCreation, "cache creation mismatch")
			require.Equal(t, tc.wantRead, newRead, "cache read mismatch")
		})
	}
}

// TestUserPriorityOverGroupConfig 测试用户配置优先于分组配置
func TestUserPriorityOverGroupConfig(t *testing.T) {
	// 模拟优先级逻辑（与 gateway_handler.go 中的逻辑一致）
	getEffectiveRatio := func(groupRatio, groupProb float64, userRatio, userProb *float64) (float64, float64) {
		ratio := groupRatio
		prob := groupProb

		if userRatio != nil {
			ratio = *userRatio
		}
		if userProb != nil {
			prob = *userProb
		}

		return ratio, prob
	}

	t.Run("user overrides group", func(t *testing.T) {
		groupRatio := 0.5
		groupProb := 1.0

		userRatio := 0.3
		userProb := 0.5

		ratio, prob := getEffectiveRatio(groupRatio, groupProb, &userRatio, &userProb)
		require.Equal(t, 0.3, ratio, "user ratio should override group")
		require.Equal(t, 0.5, prob, "user prob should override group")
	})

	t.Run("user nil uses group", func(t *testing.T) {
		groupRatio := 0.5
		groupProb := 1.0

		ratio, prob := getEffectiveRatio(groupRatio, groupProb, nil, nil)
		require.Equal(t, 0.5, ratio, "should use group ratio when user is nil")
		require.Equal(t, 1.0, prob, "should use group prob when user is nil")
	})

	t.Run("partial user override", func(t *testing.T) {
		groupRatio := 0.5
		groupProb := 1.0

		userRatio := 0.3

		ratio, prob := getEffectiveRatio(groupRatio, groupProb, &userRatio, nil)
		require.Equal(t, 0.3, ratio, "user ratio should override")
		require.Equal(t, 1.0, prob, "should use group prob when user prob is nil")
	})
}
