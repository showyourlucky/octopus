package relay

// axonhub/llm 接线层：inbound/outbound transformer 的选型与构造。
// 替换掉已删除的 internal/transformer 包。

import (
	"fmt"
	"io"
	"net/url"

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

// getOutbound 按渠道类型构造 axonhub outbound transformer。
// 注意：每次调用都重新构造，不做缓存。transformer 在构造时固化 baseURL 与 key；
// 若缓存实例，管理后台修改渠道 base_url / 轮换 Key 后，relay 会继续用旧配置直到进程重启。
// 构造开销极小（struct 初始化 + URL 校验），老代码也是按次传参，无需缓存。
func getOutbound(typ dbmodel.OutboundType, baseURL, apiKey string) (transformer.Outbound, error) {
	switch typ {
	case dbmodel.OutboundTypeOpenAIChat, dbmodel.OutboundTypeOpenAIEmbedding:
		// openai outbound 按 llm.RequestType 路由，chat 与 embedding 共用。
		return openai.NewOutboundTransformer(baseURL, apiKey)
	case dbmodel.OutboundTypeOpenAIResponse:
		return responses.NewOutboundTransformer(baseURL, apiKey)
	case dbmodel.OutboundTypeAnthropic:
		return anthropic.NewOutboundTransformer(baseURL, apiKey)
	case dbmodel.OutboundTypeGemini:
		return gemini.NewOutboundTransformer(baseURL, apiKey)
	case dbmodel.OutboundTypeVolcengine:
		// fork 的 volcengine 实际打 doubao-seed-*，对应 axonhub 的 doubao transformer。
		return doubao.NewOutboundTransformer(baseURL, apiKey)
	default:
		return nil, fmt.Errorf("unsupported channel type: %d", typ)
	}
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
