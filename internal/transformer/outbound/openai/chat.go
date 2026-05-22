package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

type ChatOutbound struct{}

func (o *ChatOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	reasoningContents := collectMimoReasoningContents(request, baseUrl)
	request.ClearHelpFields()
	restoreMimoReasoningContents(request, reasoningContents)

	// Convert developer role to system role for compatibility
	for i := range request.Messages {
		if request.Messages[i].Role == "developer" {
			request.Messages[i].Role = "system"
		}
	}

	if request.Stream != nil && *request.Stream {
		if request.StreamOptions == nil {
			request.StreamOptions = &model.StreamOptions{IncludeUsage: true}
		} else if !request.StreamOptions.IncludeUsage {
			request.StreamOptions.IncludeUsage = true
		}
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedUrl.Path = parsedUrl.Path + "/chat/completions"
	req.URL = parsedUrl
	req.Method = http.MethodPost
	return req, nil
}

type mimoReasoningContent struct {
	index int
	value string
}

func collectMimoReasoningContents(request *model.InternalLLMRequest, baseUrl string) []mimoReasoningContent {
	if request == nil || !shouldPassMimoReasoningContent(baseUrl, request.Model) {
		return nil
	}

	reasoningContents := make([]mimoReasoningContent, 0)
	for i, msg := range request.Messages {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}

		// 小米 MiMo 思考模式要求带工具调用的历史 assistant 消息完整回传 reasoning_content。
		if msg.ReasoningContent != nil {
			reasoningContents = append(reasoningContents, mimoReasoningContent{index: i, value: *msg.ReasoningContent})
			continue
		}
		if msg.Reasoning != nil {
			reasoningContents = append(reasoningContents, mimoReasoningContent{index: i, value: *msg.Reasoning})
		}
	}
	return reasoningContents
}

func restoreMimoReasoningContents(request *model.InternalLLMRequest, reasoningContents []mimoReasoningContent) {
	if request == nil || len(reasoningContents) == 0 {
		return
	}

	for _, item := range reasoningContents {
		if item.index < 0 || item.index >= len(request.Messages) {
			continue
		}
		request.Messages[item.index].ReasoningContent = &item.value
	}
}

func shouldPassMimoReasoningContent(baseUrl, modelName string) bool {
	lowerBaseUrl := strings.ToLower(baseUrl)
	lowerModelName := strings.ToLower(modelName)
	return strings.Contains(lowerBaseUrl, "xiaomimimo.com") || strings.HasPrefix(lowerModelName, "mimo-")
}

func (o *ChatOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("response body is empty")
	}

	var resp model.InternalLLMResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &resp, nil
}

func (o *ChatOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	if bytes.HasPrefix(eventData, []byte("[DONE]")) {
		return &model.InternalLLMResponse{
			Object: "[DONE]",
		}, nil
	}

	var errCheck struct {
		Error *model.ErrorDetail `json:"error"`
	}
	if err := json.Unmarshal(eventData, &errCheck); err == nil && errCheck.Error != nil {
		return nil, &model.ResponseError{
			Detail: *errCheck.Error,
		}
	}

	var resp model.InternalLLMResponse
	if err := json.Unmarshal(eventData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream chunk: %w", err)
	}
	return &resp, nil
}
