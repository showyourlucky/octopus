package openai

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestChatOutboundTransformRequestPreserveMimoReasoningContent(t *testing.T) {
	reasoningContent := "需要先调用天气工具获取实时数据。"
	request := &model.InternalLLMRequest{
		Model: "mimo-v2.5-pro",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtrForChatTest("北京天气怎么样？")}},
			{
				Role:             "assistant",
				ReasoningContent: &reasoningContent,
				ToolCalls: []model.ToolCall{
					{
						ID:   "call_weather",
						Type: "function",
						Function: model.FunctionCall{
							Name:      "get_weather",
							Arguments: `{"location":"北京"}`,
						},
					},
				},
			},
			{Role: "tool", ToolCallID: stringPtrForChatTest("call_weather"), Content: model.MessageContent{Content: stringPtrForChatTest("晴，25°C")}},
		},
	}

	httpRequest, err := (&ChatOutbound{}).TransformRequest(context.Background(), request, "https://api.xiaomimimo.com/v1", "test-key")
	if err != nil {
		t.Fatalf("构造小米 MiMo 请求失败：%v", err)
	}

	var body struct {
		Messages []model.Message `json:"messages"`
	}
	data, err := io.ReadAll(httpRequest.Body)
	if err != nil {
		t.Fatalf("读取请求体失败：%v", err)
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("解析请求体失败：%v", err)
	}

	got := body.Messages[1].ReasoningContent
	if got == nil || *got != reasoningContent {
		t.Fatalf("期望保留 reasoning_content=%q，实际为 %v", reasoningContent, got)
	}
}

func TestChatOutboundTransformRequestClearsReasoningContentForDefaultOpenAI(t *testing.T) {
	reasoningContent := "普通 OpenAI 兼容通道不应透传内部思考字段。"
	request := &model.InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []model.Message{
			{
				Role:             "assistant",
				ReasoningContent: &reasoningContent,
				ToolCalls: []model.ToolCall{
					{
						ID:   "call_default",
						Type: "function",
						Function: model.FunctionCall{
							Name:      "noop",
							Arguments: `{}`,
						},
					},
				},
			},
		},
	}

	httpRequest, err := (&ChatOutbound{}).TransformRequest(context.Background(), request, "https://api.openai.com/v1", "test-key")
	if err != nil {
		t.Fatalf("构造 OpenAI 请求失败：%v", err)
	}

	var body struct {
		Messages []model.Message `json:"messages"`
	}
	data, err := io.ReadAll(httpRequest.Body)
	if err != nil {
		t.Fatalf("读取请求体失败：%v", err)
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("解析请求体失败：%v", err)
	}

	if body.Messages[0].ReasoningContent != nil {
		t.Fatalf("普通 OpenAI 通道不应保留 reasoning_content，实际为 %q", *body.Messages[0].ReasoningContent)
	}
}

func stringPtrForChatTest(s string) *string {
	return &s
}
