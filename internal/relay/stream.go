package relay

// 流式转发（axonhub/llm 模型）：
// 上游 SSE → 原始事件流 → 错误检查(P0-1) → 出站转换(渠道协议→统一) → 入站转换(统一→客户端协议) → 写回客户端。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/openai"
	oairesponses "github.com/looplj/axonhub/llm/transformer/openai/responses"
)

// sendStreamRequest 发送流式请求，返回原始 SSE 事件流。
// >=400 状态码由 DoStream 直接返回 *httpclient.Error（含 body）。
func (ra *relayAttempt) sendStreamRequest(ctx context.Context, hcReq *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	stdClient, err := helper.ChannelHttpClient(ra.channel)
	if err != nil {
		log.Warnf("failed to get http client: %v", err)
		return nil, err
	}
	hc := httpclient.NewHttpClientWithClient(stdClient)
	return hc.DoStream(ctx, hcReq)
}

// handleStreamResponse 处理流式响应。
func (ra *relayAttempt) handleStreamResponse(ctx context.Context, hcReq *httpclient.Request) error {
	events, err := ra.sendStreamRequest(ctx, hcReq)
	if err != nil {
		return err
	}
	defer events.Close()

	// 设置 SSE 响应头
	ra.c.Header("Content-Type", "text/event-stream")
	ra.c.Header("Cache-Control", "no-cache")
	ra.c.Header("Connection", "keep-alive")
	ra.c.Header("X-Accel-Buffering", "no")

	// P0-1：在原始事件流上检查上游错误事件。
	// 错误在首字节写入前被拦截 → attempt() 判定 Written=false → 换 Key/渠道重试。
	inspected := streams.MapErr(events, func(ev *httpclient.StreamEvent) (*httpclient.StreamEvent, error) {
		if respErr := detectStreamError(ra.channel.Type, ev); respErr != nil {
			return nil, respErr
		}
		return ev, nil
	})

	// 出站转换：渠道协议 → 统一模型
	outStream, err := ra.outAdapter.TransformStream(ctx, hcReq, inspected)
	if err != nil {
		return fmt.Errorf("failed to transform outbound stream: %w", err)
	}
	defer outStream.Close()

	// 收集统一 chunk：用于有效内容判定与日志聚合（副作用，不改变流）
	var (
		hasEffectiveContent atomic.Bool
		collected           []*llm.Response
	)
	tapped := streams.Map(outStream, func(r *llm.Response) *llm.Response {
		if hasStreamResponseContent(r) {
			hasEffectiveContent.Store(true)
		}
		collected = append(collected, r)
		return r
	})

	// 入站转换：统一模型 → 客户端协议
	inStream, err := ra.inAdapter.TransformStream(ctx, tapped)
	if err != nil {
		return fmt.Errorf("failed to transform inbound stream: %w", err)
	}
	defer inStream.Close()

	firstToken := true
	pendingChunks := make([][]byte, 0, 4)
	emptyResponseIsFailure, _ := op.SettingGetBool(dbmodel.SettingKeyEmptyResponseIsFailure)

	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if ra.firstTokenTimeOutSec > 0 {
		firstTokenTimer = time.NewTimer(time.Duration(ra.firstTokenTimeOutSec) * time.Second)
		firstTokenC = firstTokenTimer.C
		defer firstTokenTimer.Stop()
	}

	flushPending := func() {
		if len(pendingChunks) == 0 {
			return
		}
		for _, p := range pendingChunks {
			_, _ = ra.c.Writer.Write(p)
		}
		pendingChunks = nil
		ra.c.Writer.Flush()
	}

	for {
		select {
		case <-ctx.Done():
			log.Infof("client disconnected, stopping stream")
			return nil
		case <-firstTokenC:
			log.Warnf("first token timeout (%ds), switching channel", ra.firstTokenTimeOutSec)
			return fmt.Errorf("first token timeout (%ds)", ra.firstTokenTimeOutSec)
		default:
		}

		if !inStream.Next() {
			if err := inStream.Err(); err != nil {
				// 流内错误（含 P0-1 拦截的上游错误事件）
				return ra.streamErr(err)
			}
			// 流正常结束
			if !hasEffectiveContent.Load() {
				if emptyResponseIsFailure {
					return fmt.Errorf("empty stream response")
				}
				flushPending()
			}
			ra.metrics.SetLLMResponse(aggregateResponses(collected), ra.llmRequest.Model)
			log.Infof("stream end")
			return nil
		}

		ev := inStream.Current()
		if ev == nil || len(ev.Data) == 0 {
			continue
		}

		if firstToken {
			ra.metrics.SetFirstTokenTime(time.Now())
			firstToken = false
			if firstTokenTimer != nil {
				if !firstTokenTimer.Stop() {
					select {
					case <-firstTokenTimer.C:
					default:
					}
				}
				firstTokenTimer = nil
				firstTokenC = nil
			}
		}

		frame := encodeSSEFrame(ev)
		if !hasEffectiveContent.Load() {
			// 在确认有效内容前，先缓冲元数据事件，避免空流误写导致后续无法重试。
			pendingChunks = append(pendingChunks, frame)
			continue
		}

		flushPending()
		_, _ = ra.c.Writer.Write(frame)
		ra.c.Writer.Flush()
	}
}

// streamErr 处理流内错误：已写过则不可重试，直接返回；
// 未写过则返回错误由 attempt() 换 Key/渠道重试。
func (ra *relayAttempt) streamErr(err error) error {
	var respErr *llm.ResponseError
	if errors.As(err, &respErr) {
		log.Warnf("upstream stream error: %v", respErr)
		return respErr
	}
	log.Warnf("failed to read stream: %v", err)
	return fmt.Errorf("failed to read stream: %w", err)
}

// encodeSSEFrame 把客户端事件编码为 SSE 帧。
func encodeSSEFrame(ev *httpclient.StreamEvent) []byte {
	var buf bytes.Buffer
	if ev.Type != "" {
		buf.WriteString("event: ")
		buf.WriteString(ev.Type)
		buf.WriteByte('\n')
	}
	// data 可能含换行，逐行加前缀
	for _, line := range bytes.Split(ev.Data, []byte("\n")) {
		buf.WriteString("data: ")
		buf.Write(line)
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}

// detectStreamError 检查原始上游事件是否为错误事件（P0-1）。
// 返回非 nil 表示该事件即失败；调用方在首字节写入前拦截则可重试。
// 模式参考上游 bestruirui/octopus internal/relay/protocol.go inspectStreamEvent。
func detectStreamError(channelType dbmodel.OutboundType, event *httpclient.StreamEvent) error {
	if event == nil || len(event.Data) == 0 {
		return nil
	}

	switch channelType {
	case dbmodel.OutboundTypeOpenAIChat, dbmodel.OutboundTypeVolcengine:
		if bytes.Equal(event.Data, llm.DoneStreamEvent.Data) {
			return nil
		}
		var failure openai.OpenAIError
		if err := json.Unmarshal(event.Data, &failure); err != nil {
			return nil // 不是错误 JSON，按正常事件放行（转换层会处理）
		}
		if event.Type == "error" || failure.Detail.Message != "" || failure.Detail.Type != "" || failure.Detail.Code != "" {
			if failure.Detail.Message == "" {
				failure.Detail.Message = "openai stream error"
			}
			return &llm.ResponseError{Detail: failure.Detail}
		}
		return nil

	case dbmodel.OutboundTypeOpenAIResponse:
		var parsed oairesponses.StreamEvent
		if err := json.Unmarshal(event.Data, &parsed); err != nil {
			return nil
		}
		switch parsed.Type {
		case oairesponses.StreamEventTypeResponseFailed:
			if parsed.Response != nil && parsed.Response.Error != nil {
				return &llm.ResponseError{Detail: llm.ErrorDetail{Code: parsed.Response.Error.Code, Message: parsed.Response.Error.Message, Type: parsed.Response.Error.Type}}
			}
			return &llm.ResponseError{Detail: llm.ErrorDetail{Message: "response failed", Type: "response_failed"}}
		case oairesponses.StreamEventTypeResponseIncomplete:
			message := "response incomplete"
			if parsed.Response != nil && parsed.Response.IncompleteDetails != nil && parsed.Response.IncompleteDetails.Reason != "" {
				message += ": " + parsed.Response.IncompleteDetails.Reason
			}
			return &llm.ResponseError{Detail: llm.ErrorDetail{Message: message, Type: "response_incomplete"}}
		case oairesponses.StreamEventTypeResponseCancelled:
			return &llm.ResponseError{Detail: llm.ErrorDetail{Message: "response cancelled", Type: "response_cancelled"}}
		case oairesponses.StreamEventTypeError:
			if parsed.Message == "" {
				parsed.Message = "responses stream error"
			}
			return &llm.ResponseError{Detail: llm.ErrorDetail{Code: parsed.Code, Message: parsed.Message, Type: "stream_error"}}
		case oairesponses.StreamEventTypeResponseCompleted:
			// completed 事件仍可能携带非 completed 终态
			if parsed.Response != nil && parsed.Response.Status != nil && *parsed.Response.Status != "" && *parsed.Response.Status != "completed" {
				if parsed.Response.Error != nil {
					return &llm.ResponseError{Detail: llm.ErrorDetail{Code: parsed.Response.Error.Code, Message: parsed.Response.Error.Message, Type: parsed.Response.Error.Type}}
				}
				return &llm.ResponseError{Detail: llm.ErrorDetail{Message: "response " + *parsed.Response.Status, Type: "response_" + *parsed.Response.Status}}
			}
			return nil
		default:
			return nil
		}

	case dbmodel.OutboundTypeAnthropic:
		var parsed anthropic.StreamEvent
		if err := json.Unmarshal(event.Data, &parsed); err != nil {
			return nil
		}
		if parsed.Type == "" || (event.Type != "" && event.Type != parsed.Type) {
			return nil // 交给转换层处理
		}
		if parsed.Type == "error" {
			var failure anthropic.AnthropicError
			if err := json.Unmarshal(event.Data, &failure); err != nil {
				return &llm.ResponseError{Detail: llm.ErrorDetail{Message: "anthropic stream error", Type: "stream_error"}}
			}
			if failure.Error.Message == "" {
				failure.Error.Message = "anthropic stream error"
			}
			return &llm.ResponseError{Detail: llm.ErrorDetail{Message: failure.Error.Message, Type: failure.Error.Type, RequestID: failure.RequestID}}
		}
		return nil

	default:
		return nil
	}
}

// aggregateResponses 聚合流式统一 chunk，用于日志与用量统计（近似即可，不影响客户端）。
func aggregateResponses(chunks []*llm.Response) *llm.Response {
	if len(chunks) == 0 {
		return nil
	}
	result := &llm.Response{Object: "chat.completion"}
	choicesMap := make(map[int]*llm.Choice)
	var choiceOrder []int
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		if result.ID == "" && chunk.ID != "" {
			result.ID = chunk.ID
		}
		if result.Model == "" && chunk.Model != "" {
			result.Model = chunk.Model
		}
		if result.Created == 0 && chunk.Created != 0 {
			result.Created = chunk.Created
		}
		if chunk.Usage != nil {
			result.Usage = chunk.Usage
		}
		for _, c := range chunk.Choices {
			cc, ok := choicesMap[c.Index]
			if !ok {
				cc = &llm.Choice{Index: c.Index}
				choicesMap[c.Index] = cc
				choiceOrder = append(choiceOrder, c.Index)
			}
			if c.Message != nil {
				if cc.Message == nil {
					m := *c.Message
					cc.Message = &m
				} else {
					mergeMessage(cc.Message, c.Message)
				}
			}
			if c.Delta != nil {
				if cc.Delta == nil {
					d := *c.Delta
					cc.Delta = &d
				} else {
					mergeMessage(cc.Delta, c.Delta)
				}
			}
			if c.FinishReason != nil {
				cc.FinishReason = c.FinishReason
			}
		}
	}
	for _, idx := range choiceOrder {
		result.Choices = append(result.Choices, *choicesMap[idx])
	}
	return result
}

// mergeMessage 把增量消息内容拼接到已有消息上（日志聚合用）。
func mergeMessage(dst, src *llm.Message) {
	if src.Content.Content != nil {
		if dst.Content.Content == nil {
			s := ""
			dst.Content.Content = &s
		}
		*dst.Content.Content += *src.Content.Content
	}
	dst.Content.MultipleContent = append(dst.Content.MultipleContent, src.Content.MultipleContent...)
	dst.ToolCalls = append(dst.ToolCalls, src.ToolCalls...)
	if src.ReasoningContent != nil {
		if dst.ReasoningContent == nil {
			s := ""
			dst.ReasoningContent = &s
		}
		*dst.ReasoningContent += *src.ReasoningContent
	}
	if dst.Refusal == "" {
		dst.Refusal = src.Refusal
	}
}

// handleResponse 处理非流式响应。
func (ra *relayAttempt) handleResponse(ctx context.Context, hcReq *httpclient.Request, hcResp *httpclient.Response) error {
	llmResp, err := ra.outAdapter.TransformResponse(ctx, hcResp)
	if err != nil {
		// 出站转换失败：尝试转为统一错误，用于重试判定
		if respErr := toResponseError(err); respErr != nil {
			return respErr
		}
		log.Warnf("failed to transform outbound response: %v", err)
		return fmt.Errorf("failed to transform outbound response: %w", err)
	}

	inResp, err := ra.inAdapter.TransformResponse(ctx, llmResp)
	if err != nil {
		log.Warnf("failed to transform inbound response: %v", err)
		return fmt.Errorf("failed to transform inbound response: %w", err)
	}

	if ra.suppressResponse {
		ra.metrics.SetLLMResponse(llmResp, ra.llmRequest.Model)
		return nil
	}

	ra.metrics.SetLLMResponse(llmResp, ra.llmRequest.Model)
	ra.c.Data(http.StatusOK, "application/json", inResp.Body)
	return nil
}

// collectResponse 收集非流式响应信息（成功路径；流式在 handleStreamResponse 内已聚合）。
func (ra *relayAttempt) collectResponse() {
	// 非流式已在 handleResponse 中设置；流式已在 handleStreamResponse 中聚合。
	// 兜底：已写入但未设置时不再补救（避免重复统计）。
}

// toResponseError 尝试把出站转换错误转为统一错误模型。
func toResponseError(err error) *llm.ResponseError {
	var respErr *llm.ResponseError
	if errors.As(err, &respErr) {
		return respErr
	}
	return nil
}
