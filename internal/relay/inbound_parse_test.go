package relay

import (
	"context"
	"testing"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestInboundParse(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":true,"temperature":0.7}`)
	in := getInbound(InboundTypeOpenAIChat)
	req, err := in.TransformRequest(context.Background(), &httpclient.Request{
		Method:  "POST",
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    body,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if req.Model != "gpt-4o" || req.Stream == nil || !*req.Stream || len(req.Messages) != 1 {
		t.Errorf("parsed wrong: %+v", req)
	}
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Errorf("temperature lost")
	}
	t.Logf("model=%s stream=%v msgs=%d temp=%v", req.Model, *req.Stream, len(req.Messages), *req.Temperature)

	// anthropic inbound
	abody := []byte(`{"model":"claude-sonnet-4-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	ain := getInbound(InboundTypeAnthropic)
	areq, err := ain.TransformRequest(context.Background(), &httpclient.Request{
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    abody,
	})
	if err != nil {
		t.Fatalf("anthropic parse: %v", err)
	}
	if areq.Model != "claude-sonnet-4-5" || len(areq.Messages) != 1 {
		t.Errorf("anthropic parsed wrong: %+v", areq)
	}
	t.Logf("anthropic model=%s msgs=%d", areq.Model, len(areq.Messages))
}
