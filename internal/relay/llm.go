package relay

// axonhub/llm 接线层：inbound/outbound transformer 的选型与构造。
// 替换掉已删除的 internal/transformer 包。

import (
	"fmt"
	"io"
	"net/url"
	"sync"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/doubao"
	"github.com/looplj/axonhub/llm/transformer/gemini"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/httpclient"
)

// InboundType 入站协议类型（对应 /v1 下的四个端点）。
type InboundType int

const (
	InboundTypeOpenAIChat InboundType = iota
	InboundTypeOpenAIResponse
	InboundTypeAnthropic
	InboundTypeOpenAIEmbedding
)

// inbound transformer 无状态，用单例。
var (
	inboundOpenAIChat      = openai.NewInboundTransformer()
	inboundOpenAIResponses = responses.NewInboundTransformer()
	inboundAnthropic       = anthropic.NewInboundTransformer()
	inboundOpenAIEmbedding = openai.NewEmbeddingInboundTransformer()
)

// getInbound 按入站协议类型返回 axonhub inbound transformer。
func getInbound(t InboundType) transformer.Inbound {
	switch t {
	case InboundTypeOpenAIChat:
		return inboundOpenAIChat
	case InboundTypeOpenAIResponse:
		return inboundOpenAIResponses
	case InboundTypeAnthropic:
		return inboundAnthropic
	case InboundTypeOpenAIEmbedding:
		return inboundOpenAIEmbedding
	default:
		return nil
	}
}

// outboundCacheKey 出站 transformer 缓存键。
// transformer 在构造时固化 baseURL 与 key（经 Config 注入），
// 多端点/多 Key 场景下每个组合一个实例。
type outboundCacheKey struct {
	typ     dbmodel.OutboundType
	baseURL string
	apiKey  string
}

var outboundCache sync.Map // map[outboundCacheKey]transformer.Outbound

// getOutbound 按渠道类型构造（或取缓存）axonhub outbound transformer。
func getOutbound(typ dbmodel.OutboundType, baseURL, apiKey string) (transformer.Outbound, error) {
	key := outboundCacheKey{typ: typ, baseURL: baseURL, apiKey: apiKey}
	if v, ok := outboundCache.Load(key); ok {
		return v.(transformer.Outbound), nil
	}

	var (
		out transformer.Outbound
		err error
	)
	switch typ {
	case dbmodel.OutboundTypeOpenAIChat, dbmodel.OutboundTypeOpenAIEmbedding:
		// openai outbound 按 llm.RequestType 路由，chat 与 embedding 共用。
		out, err = openai.NewOutboundTransformer(baseURL, apiKey)
	case dbmodel.OutboundTypeOpenAIResponse:
		out, err = responses.NewOutboundTransformer(baseURL, apiKey)
	case dbmodel.OutboundTypeAnthropic:
		out, err = anthropic.NewOutboundTransformer(baseURL, apiKey)
	case dbmodel.OutboundTypeGemini:
		out, err = gemini.NewOutboundTransformer(baseURL, apiKey)
	case dbmodel.OutboundTypeVolcengine:
		// fork 的 volcengine 实际打 doubao-seed-*，对应 axonhub 的 doubao transformer。
		out, err = doubao.NewOutboundTransformer(baseURL, apiKey)
	default:
		return nil, fmt.Errorf("unsupported channel type: %d", typ)
	}
	if err != nil {
		return nil, err
	}
	outboundCache.Store(key, out)
	return out, nil
}

// toHTTPClientRequest 把 gin 入站请求包成 axonhub inbound 吃的 httpclient.Request。
// inbound TransformRequest 只读 Body 与 Headers（Content-Type 校验），其余字段为透传/日志用。
func toHTTPClientRequest(c *gin.Context, body []byte) *httpclient.Request {
	req := c.Request
	return &httpclient.Request{
		Method:     req.Method,
		URL:        req.URL.String(),
		Path:       req.URL.Path,
		Query:      req.URL.Query(),
		Headers:    req.Header,
		Body:       body,
		RawRequest: req,
	}
}

// readBody 读完并恢复 gin 请求体（inbound 只读一次，这里统一处理）。
func readBody(c *gin.Context) ([]byte, error) {
	return io.ReadAll(c.Request.Body)
}

// validateLLMRequest 校验统一请求（沿用 validator tag，与旧 InternalLLMRequest.Validate 一致）。
func validateLLMRequest(req *llm.Request) error {
	return validator.New().Struct(req)
}

// applyQueryPassthrough 把客户端原始 query 参数拼到出站 URL 上。
// 行为与旧 transformer 一致：仅 Anthropic / Gemini 出站做透传。
func applyQueryPassthrough(typ dbmodel.OutboundType, hcReq *httpclient.Request, query url.Values) {
	if len(query) == 0 {
		return
	}
	if typ != dbmodel.OutboundTypeAnthropic && typ != dbmodel.OutboundTypeGemini {
		return
	}
	u, err := url.Parse(hcReq.URL)
	if err != nil {
		return
	}
	q := u.Query()
	for k, vs := range query {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()
	hcReq.URL = u.String()
}
