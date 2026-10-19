package relay

import (
	"context"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestStreamRoundTrip(t *testing.T) {
	ctx := context.Background()
	content := "hello"
	stream := true
	llmReq := &llm.Request{
		Model:  "gpt-4o",
		Stream: &stream,
		Messages: []llm.Message{
			{Role: "user", Content: llm.MessageContent{Content: &content}},
		},
	}
	out, _ := getOutbound(dbmodel.OutboundTypeOpenAIChat, "https://api.openai.com", "sk-test")
	hcReq, err := out.TransformRequest(ctx, llmReq)
	if err != nil {
		t.Fatal(err)
	}

	// 模拟上游 SSE 事件（provider 格式）
	rawEvents := []*httpclient.StreamEvent{
		{Data: []byte(`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`)},
		{Data: []byte(`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)},
		{Data: []byte(`[DONE]`)},
	}
	outStream, err := out.TransformStream(ctx, hcReq, streams.SliceStream(rawEvents))
	if err != nil {
		t.Fatalf("outbound TransformStream: %v", err)
	}
	in := getInbound(InboundTypeOpenAIChat)
	inStream, err := in.TransformStream(ctx, outStream)
	if err != nil {
		t.Fatalf("inbound TransformStream: %v", err)
	}
	n := 0
	for inStream.Next() {
		ev := inStream.Current()
		t.Logf("client event: %s", string(ev.Data))
		n++
	}
	if err := inStream.Err(); err != nil {
		t.Fatalf("stream err: %v", err)
	}
	if n == 0 {
		t.Error("no client events produced")
	}
}
