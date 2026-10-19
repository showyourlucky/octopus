package relay

// 迁移冒烟测试：验证 axonhub/llm 接线的基本正确性。
// 跑通后可保留作为回归测试。

import (
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestGetOutboundAllTypes(t *testing.T) {
	types := []dbmodel.OutboundType{
		dbmodel.OutboundTypeOpenAIChat,
		dbmodel.OutboundTypeOpenAIResponse,
		dbmodel.OutboundTypeAnthropic,
		dbmodel.OutboundTypeGemini,
		dbmodel.OutboundTypeVolcengine,
		dbmodel.OutboundTypeOpenAIEmbedding,
	}
	for _, typ := range types {
		out, err := getOutbound(typ, "https://example.com", "sk-test")
		if err != nil {
			t.Errorf("type %d: unexpected error: %v", typ, err)
			continue
		}
		if out == nil {
			t.Errorf("type %d: nil outbound", typ)
			continue
		}
		if out.APIFormat() == "" {
			t.Errorf("type %d: empty APIFormat", typ)
		}
		t.Logf("type %d -> %s", typ, out.APIFormat())
	}

	if _, err := getOutbound(dbmodel.OutboundType(999), "https://example.com", "sk"); err == nil {
		t.Error("expected error for unknown type")
	}
}

func TestGetInboundAllTypes(t *testing.T) {
	types := []InboundType{
		InboundTypeOpenAIChat,
		InboundTypeOpenAIResponse,
		InboundTypeAnthropic,
		InboundTypeOpenAIEmbedding,
	}
	for _, typ := range types {
		if getInbound(typ) == nil {
			t.Errorf("type %d: nil inbound", typ)
		}
	}
}

func TestDetectStreamError(t *testing.T) {
	cases := []struct {
		name      string
		typ       dbmodel.OutboundType
		data      string
		wantError bool
	}{
		{"openai done", dbmodel.OutboundTypeOpenAIChat, `[DONE]`, false},
		{"openai chunk", dbmodel.OutboundTypeOpenAIChat, `{"choices":[{"delta":{"content":"hi"}}]}`, false},
		{"openai error", dbmodel.OutboundTypeOpenAIChat, `{"error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limited"}}`, true},
		{"responses failed", dbmodel.OutboundTypeOpenAIResponse, `{"type":"response.failed","response":{"error":{"code":"500","message":"boom"}}}`, true},
		{"responses incomplete", dbmodel.OutboundTypeOpenAIResponse, `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_tokens"}}}`, true},
		{"responses completed ok", dbmodel.OutboundTypeOpenAIResponse, `{"type":"response.completed","response":{"status":"completed"}}`, false},
		{"anthropic error", dbmodel.OutboundTypeAnthropic, `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`, true},
		{"anthropic stop", dbmodel.OutboundTypeAnthropic, `{"type":"message_stop"}`, false},
		{"empty", dbmodel.OutboundTypeOpenAIChat, ``, false},
	}
	for _, c := range cases {
		err := detectStreamError(c.typ, &httpclient.StreamEvent{Data: []byte(c.data)})
		if (err != nil) != c.wantError {
			t.Errorf("%s: got err=%v, wantError=%v", c.name, err, c.wantError)
		}
	}
}

func TestEncodeSSEFrame(t *testing.T) {
	frame := encodeSSEFrame(&httpclient.StreamEvent{Data: []byte(`{"a":1}`)})
	expected := "data: {\"a\":1}\n\n"
	if string(frame) != expected {
		t.Errorf("got %q, want %q", frame, expected)
	}
	frame2 := encodeSSEFrame(&httpclient.StreamEvent{Type: "error", Data: []byte(`{}`)})
	if string(frame2) != "event: error\ndata: {}\n\n" {
		t.Errorf("got %q", frame2)
	}
}
