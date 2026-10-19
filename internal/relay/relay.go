package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
)

// Handler 处理入站请求并转发到上游服务
func Handler(inboundType InboundType, c *gin.Context) {
	// 解析请求
	llmReq, inAdapter, err := parseRequest(inboundType, c)
	if err != nil {
		return
	}
	supportedModels := c.GetString("supported_models")

	requestModel := llmReq.Model
	apiKeyID := c.GetInt("api_key_id")
	apiKey, err := op.APIKeyGet(apiKeyID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	apiKey.SupportedModels = supportedModels
	if !op.APIKeySupportsModel(apiKey, requestModel) {
		resp.Error(c, http.StatusBadRequest, "model not supported")
		return
	}

	// 获取通道分组
	group, err := op.APIKeyResolveGroup(apiKey, requestModel, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, "model not found")
		return
	}

	// 创建迭代器（策略排序 + 粘性优先）
	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		resp.Error(c, http.StatusServiceUnavailable, "no available channel")
		return
	}

	// 初始化 Metrics
	metrics := NewRelayMetrics(apiKeyID, requestModel, llmReq)

	// 请求级上下文
	req := &relayRequest{
		c:           c,
		inAdapter:   inAdapter,
		llmRequest:  llmReq,
		metrics:     metrics,
		apiKeyID:    apiKeyID,
		requestModel: requestModel,
		group:       group,
		iter:        iter,
	}

	result := executeRelay(req)
	if result.Success || result.Written || result.Err == context.Canceled {
		return
	}

	resp.Error(c, http.StatusBadGateway, "all channels failed")
}

func executeRelay(req *relayRequest) attemptResult {
	c := req.c
	llmReq := req.llmRequest
	metrics := req.metrics
	iter := req.iter
	requestModel := req.requestModel
	group := req.group

	var lastErr error

	for iter.Next() {
		select {
		case <-c.Request.Context().Done():
			log.Infof("request context canceled, stopping retry")
			metrics.Save(c.Request.Context(), false, context.Canceled, iter.Attempts())
			return attemptResult{Err: context.Canceled}
		default:
		}

		item := iter.Item()

		// 获取通道
		channel, err := op.ChannelGet(item.ChannelID, c.Request.Context())
		if err != nil {
			log.Warnf("failed to get channel %d: %v", item.ChannelID, err)
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), "", fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "", "channel disabled")
			continue
		}

		// 类型兼容性检查（embedding 请求只能走 embedding 渠道）
		isEmbedding := llmReq.Embedding != nil
		if isEmbedding && !dbmodel.IsEmbeddingChannelType(channel.Type) {
			iter.Skip(channel.ID, 0, channel.Name, "", "channel type not compatible with embedding request")
			continue
		}
		if !isEmbedding && !dbmodel.IsChatChannelType(channel.Type) {
			iter.Skip(channel.ID, 0, channel.Name, "", "channel type not compatible with chat request")
			continue
		}

		candidateKeys := channel.GetCandidateKeys()
		if len(candidateKeys) == 0 {
			iter.Skip(channel.ID, 0, channel.Name, "", "no available key")
			continue
		}

		// forceKeyID 用于渠道测试场景：强制锁定到指定 Key。
		// 必须在此处过滤而不是 handler 层——上方的 op.ChannelGet 会从缓存重新加载完整 channel，
		// 任何在 handler 层对 channel.Keys 的收敛都会被这次 reload 覆盖。
		// 锁定后同时把 maxAttempts 收敛为 1，避免渠道 EnableMultiKeyRetry 让其他 Key 接力。
		if req.forceKeyID != nil {
			var matched *dbmodel.ChannelKey
			for i := range candidateKeys {
				if candidateKeys[i].ID == *req.forceKeyID {
					matched = &candidateKeys[i]
					break
				}
			}
			if matched == nil {
				// 注意：GetCandidateKeys 会按 429 冷却过滤候选 Key，handler 层只校验了 Enabled，
				// 因此这里有可能因冷却而找不到。失败信息走 attempts，由 channelProbeFailureMessage 兜底。
				iter.Skip(channel.ID, *req.forceKeyID, channel.Name, "",
					fmt.Sprintf("指定 Key (id=%d) 不在候选列表（可能处于 429 冷却期）", *req.forceKeyID))
				continue
			}
			candidateKeys = []dbmodel.ChannelKey{*matched}
		}

		maxAttempts := 1
		if channel.EnableMultiKeyRetry && req.forceKeyID == nil {
			maxAttempts = channel.RetryCount
			if maxAttempts < 1 {
				maxAttempts = 1
			}
			if maxAttempts > len(candidateKeys) {
				maxAttempts = len(candidateKeys)
			}
		}

		for i := 0; i < maxAttempts; i++ {
			usedKey := candidateKeys[i]

			// 熔断检查
			if iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name, usedKey.Remark) {
				continue
			}

			// 设置实际模型
			llmReq.Model = item.ModelName

			log.Infof("request model %s, mode: %d, forwarding to channel: %s model: %s key_id: %d (attempt %d/%d, key attempt %d/%d, sticky=%t)",
				requestModel, group.Mode, channel.Name, item.ModelName, usedKey.ID,
				iter.Index()+1, iter.Len(), i+1, maxAttempts, iter.IsSticky())

			// 构造出站 transformer（按渠道类型 + 本轮 baseURL + Key，带缓存）
			outAdapter, err := getOutbound(channel.Type, channel.GetBaseUrl(), usedKey.ChannelKey)
			if err != nil {
				iter.Skip(channel.ID, usedKey.ID, channel.Name, usedKey.Remark, fmt.Sprintf("unsupported channel type: %d", channel.Type))
				continue
			}

			// 构造尝试级上下文
			ra := &relayAttempt{
				relayRequest:         req,
				outAdapter:           outAdapter,
				channel:              channel,
				usedKey:              usedKey,
				firstTokenTimeOutSec: group.FirstTokenTimeOut,
				nonStreamTimeOutSec:  group.NonStreamTimeOut,
			}

			result := ra.attempt()
			if result.Success {
				metrics.Save(c.Request.Context(), true, nil, iter.Attempts())
				return result
			}
			if result.Written {
				metrics.Save(c.Request.Context(), false, result.Err, iter.Attempts())
				return result
			}
			lastErr = result.Err
		}
	}

	// 所有通道都失败
	metrics.Save(c.Request.Context(), false, lastErr, iter.Attempts())
	if lastErr == nil {
		lastErr = fmt.Errorf("all channels failed")
	}
	return attemptResult{Err: lastErr}
}

// attempt 统一管理一次通道尝试的完整生命周期
func (ra *relayAttempt) attempt() attemptResult {
	span := ra.iter.StartAttempt(ra.channel.ID, ra.usedKey.ID, ra.channel.Name, ra.usedKey.Remark)

	// 转发请求
	statusCode, fwdErr := ra.forward()

	// 更新 channel key 状态
	ra.usedKey.StatusCode = statusCode
	ra.usedKey.LastUseTimeStamp = time.Now().Unix()

	if fwdErr == nil {
		// ====== 成功 ======
		ra.collectResponse()
		ra.usedKey.TotalCost += ra.metrics.Stats.InputCost + ra.metrics.Stats.OutputCost
		op.ChannelKeyUpdate(ra.usedKey)

		span.End(dbmodel.AttemptSuccess, statusCode, "")
		if ra.group.RuntimeFailoverOnRecentFailure {
			balancer.ClearRecentModelFailure(ra.channel.ID, ra.llmRequest.Model)
		}

		// Channel 维度统计
		op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
			WaitTime:       span.Duration().Milliseconds(),
			RequestSuccess: 1,
		})

		// 熔断器：记录成功
		balancer.RecordSuccess(ra.channel.ID, ra.usedKey.ID, ra.llmRequest.Model)
		// 会话保持：更新粘性记录
		if !ra.skipSticky {
			balancer.SetSticky(ra.apiKeyID, ra.requestModel, ra.channel.ID, ra.usedKey.ID)
		}

		ra.metrics.ParamOverride = paramOverrideValue(ra.channel.ParamOverride)

		return attemptResult{Success: true}
	}

	// ====== 失败 ======
	op.ChannelKeyUpdate(ra.usedKey)
	span.End(dbmodel.AttemptFailed, statusCode, fwdErr.Error())
	if ra.group.RuntimeFailoverOnRecentFailure {
		balancer.RecordRecentModelFailure(ra.channel.ID, ra.llmRequest.Model)
	}

	// Channel 维度统计
	op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
		WaitTime:      span.Duration().Milliseconds(),
		RequestFailed: 1,
	})

	// 熔断器：记录失败
	failures := balancer.RecordFailure(ra.channel.ID, ra.usedKey.ID, ra.llmRequest.Model)

	// 自动禁用 Key 检查
	if ra.channel.AutoBanKeyFailures > 0 && failures >= int64(ra.channel.AutoBanKeyFailures) {
		ra.usedKey.Enabled = false
		ra.usedKey.AutoBanCount++
		ra.usedKey.DisabledAt = time.Now().Unix()
		ra.usedKey.Remark += fmt.Sprintf(" [Auto banned: %d failures]", failures)
		op.ChannelKeyUpdate(ra.usedKey)
		log.Warnf("channel key %d disabled due to excessive failures (%d >= %d)",
			ra.usedKey.ID, failures, ra.channel.AutoBanKeyFailures)
	}

	ra.metrics.ParamOverride = paramOverrideValue(ra.channel.ParamOverride)

	written := ra.c.Writer.Written()
	if written {
		ra.collectResponse()
	}
	return attemptResult{
		Success: false,
		Written: written,
		Err:     fmt.Errorf("channel %s(key_id=%d) failed: %v", ra.channel.Name, ra.usedKey.ID, fwdErr),
	}
}

// parseRequest 解析并验证入站请求，返回 axonhub 统一模型。
func parseRequest(inboundType InboundType, c *gin.Context) (*llm.Request, transformer.Inbound, error) {
	body, err := readBody(c)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, err
	}

	inAdapter := getInbound(inboundType)
	if inAdapter == nil {
		resp.Error(c, http.StatusBadRequest, fmt.Sprintf("unsupported inbound type: %d", inboundType))
		return nil, nil, fmt.Errorf("unsupported inbound type: %d", inboundType)
	}
	llmReq, err := inAdapter.TransformRequest(c.Request.Context(), toHTTPClientRequest(c, body))
	if err != nil {
		// 入站请求体解析失败属于调用方错误（例如字段类型不符合协议），返回 400 而非 500，
		// 避免把客户端问题误报成服务端内部错误，干扰排障。
		resp.Error(c, http.StatusBadRequest, err.Error())
		return nil, nil, err
	}

	if err := validateLLMRequest(llmReq); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return nil, nil, err
	}

	return llmReq, inAdapter, nil
}

// forward 转发请求到上游服务
func (ra *relayAttempt) forward() (int, error) {
	ctx := ra.c.Request.Context()
	llmReq := ra.llmRequest
	isStream := llmReq.Stream != nil && *llmReq.Stream
	if !isStream && ra.nonStreamTimeOutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(ra.nonStreamTimeOutSec)*time.Second)
		defer cancel()
	}
	// 构建出站请求（baseURL 与 key 已在 getOutbound 构造 transformer 时注入）
	hcReq, err := ra.outAdapter.TransformRequest(ctx, llmReq)
	if err != nil {
		log.Warnf("failed to create request: %v", err)
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	ra.hcReq = hcReq

	// 客户端原始 query 参数透传（行为与旧 transformer 一致：仅 Anthropic / Gemini）
	applyQueryPassthrough(ra.channel.Type, hcReq, ra.c.Request.URL.Query())

	// 应用 ParamOverride 到请求体（JSON 合并，行为与旧版一致）
	if ra.channel.ParamOverride != nil && *ra.channel.ParamOverride != "" {
		body := hcReq.Body
		var bodyMap map[string]any
		if err := json.Unmarshal(body, &bodyMap); err != nil {
			log.Warnf("failed to unmarshal request body: %v, skipping param_override", err)
		} else {
			var override map[string]any
			if err := json.Unmarshal([]byte(*ra.channel.ParamOverride), &override); err != nil {
				log.Warnf("failed to unmarshal param_override: %v, skipping", err)
			} else {
				maps.Copy(bodyMap, override)
				if modifiedBody, err := json.Marshal(bodyMap); err != nil {
					log.Warnf("failed to marshal modified body: %v, skipping param_override", err)
				} else {
					hcReq.Body = modifiedBody
				}
			}
		}
	}

	// 复制请求头：客户端透传头 + 渠道自定义头
	ra.copyHeaders(hcReq)

	// 发送请求
	if isStream {
		// 流式：DoStream 直接返回事件流（含 >=400 错误处理）
		if err := ra.handleStreamResponse(ctx, hcReq); err != nil {
			return 0, err
		}
		return http.StatusOK, nil
	}
	hcResp, err := ra.sendRequest(ctx, hcReq)
	if err != nil {
		return 0, wrapNonStreamTimeoutErr(fmt.Errorf("failed to send request: %w", err), ra.nonStreamTimeOutSec)
	}

	// 处理非流式响应
	if err := ra.handleResponse(ctx, hcReq, hcResp); err != nil {
		return 0, wrapNonStreamTimeoutErr(err, ra.nonStreamTimeOutSec)
	}
	return hcResp.StatusCode, nil
}

// copyHeaders 复制请求头，过滤 hop-by-hop 头；再叠加渠道自定义头。
func (ra *relayAttempt) copyHeaders(hcReq *httpclient.Request) {
	if hcReq.Headers == nil {
		hcReq.Headers = make(http.Header)
	}
	for key, values := range ra.c.Request.Header {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			hcReq.Headers.Set(key, value)
		}
	}
	if len(ra.channel.CustomHeader) > 0 {
		for _, header := range ra.channel.CustomHeader {
			hcReq.Headers.Set(header.HeaderKey, header.HeaderValue)
		}
	}
}

// sendRequest 发送 HTTP 请求（沿用渠道代理配置）。
func (ra *relayAttempt) sendRequest(ctx context.Context, hcReq *httpclient.Request) (*httpclient.Response, error) {
	stdClient, err := helper.ChannelHttpClient(ra.channel)
	if err != nil {
		log.Warnf("failed to get http client: %v", err)
		return nil, err
	}
	hc := httpclient.NewHttpClientWithClient(stdClient)

	hcResp, err := hc.Do(ctx, hcReq)
	if err != nil {
		// hc.Do 对 >=400 状态码返回 *httpclient.Error（含状态码与 body），
		// 调用方按 ResponseError 语义处理，支持重试判定。
		var hcErr *httpclient.Error
		if errors.As(err, &hcErr) {
			return nil, hcErr
		}
		log.Warnf("failed to send request: %v", err)
		return nil, err
	}

	return hcResp, nil
}

func paramOverrideValue(ptr *string) string {
	if ptr == nil || *ptr == "" {
		return ""
	}
	return *ptr
}

func wrapNonStreamTimeoutErr(err error, timeoutSec int) error {
	if err == nil || timeoutSec <= 0 || !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	log.Warnf("non-stream timeout (%ds), switching channel", timeoutSec)
	return fmt.Errorf("non-stream timeout (%ds)", timeoutSec)
}
