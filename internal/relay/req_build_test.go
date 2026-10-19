package relay

import (
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/looplj/axonhub/llm"
)

func TestOutboundTransformRequest(t *testing.T) {
	content := "hi"
	stream := false
	req := &llm.Request{
		Model:  "gpt-4o",
		Stream: &stream,
		Messages: []llm.Message{
			{Role: "user", Content: llm.MessageContent{Content: &content}},
		},
	}
	cases := []struct {
		typ     dbmodel.OutboundType
		baseURL string
	}{
		{dbmodel.OutboundTypeOpenAIChat, "https://api.openai.com"},
		{dbmodel.OutboundTypeOpenAIResponse, "https://api.openai.com"},
		{dbmodel.OutboundTypeAnthropic, "https://api.anthropic.com"},
		{dbmodel.OutboundTypeGemini, "https://generativelanguage.googleapis.com"},
		{dbmodel.OutboundTypeVolcengine, "https://ark.cn-beijing.volces.com"},
	}
	for _, c := range cases {
		out, err := getOutbound(c.typ, c.baseURL, "sk-test")
		if err != nil {
			t.Fatalf("type %d: %v", c.typ, err)
		}
		hcReq, err := out.TransformRequest(t.Context(), req)
		if err != nil {
			t.Errorf("type %d TransformRequest: %v", c.typ, err)
			continue
		}
		t.Logf("type %d -> %s %s auth=%v body=%dB", c.typ, hcReq.Method, hcReq.URL, hcReq.Auth != nil, len(hcReq.Body))
		if hcReq.Auth == nil || hcReq.Auth.APIKey != "sk-test" {
			t.Errorf("type %d: auth not injected", c.typ)
		}
	}
}
