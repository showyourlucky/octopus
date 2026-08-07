package relay

import (
	"os"
	"strconv"
	"strings"

	"github.com/bestruirui/octopus/internal/conf"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/gin-gonic/gin"
)

// maxSSEEventSize 定义 SSE 事件的最大大小。
// 对于图像生成模型（如 gemini-3-pro-image-preview），返回的 base64 编码图像数据
// 可能非常大（高分辨率图像可能超过 10MB），因此需要设置足够大的缓冲区。
// 默认 32MB，可通过环境变量 OCTOPUS_RELAY_MAX_SSE_EVENT_SIZE 覆盖。
var maxSSEEventSize = 32 * 1024 * 1024

func init() {
	if raw := strings.TrimSpace(os.Getenv(strings.ToUpper(conf.APP_NAME) + "_RELAY_MAX_SSE_EVENT_SIZE")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			maxSSEEventSize = v
		}
	}
}

// hopByHopHeaders 定义不应转发的 HTTP 头
// 除标准 hop-by-hop 头外，还必须过滤浏览器环境头：
// 管理端渠道模型测试等请求由浏览器发起（fetch 会带 Origin/Referer/Sec-Fetch-*/Cookie），
// copyHeaders 会把这些头原样转发到上游；若上游服务做 CSRF 校验（校验 Origin/Referer/
// Sec-Fetch-* 或 Cookie 会话），会把带这些头的请求误判为浏览器请求并返回 403 CSRF_INVALID，
// 而真实 API 调用（curl/SDK）不带这些头所以正常。
var hopByHopHeaders = map[string]bool{
	"authorization":       true,
	"x-api-key":           true,
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
	"content-length":      true,
	"host":                true,
	"accept-encoding":     true,
	"x-forwarded-for":     true,
	"x-forwarded-host":    true,
	"x-forwarded-proto":   true,
	"x-forwarded-port":    true,
	"x-real-ip":           true,
	"forwarded":           true,
	"cf-connecting-ip":    true,
	"true-client-ip":      true,
	"x-client-ip":         true,
	"x-cluster-client-ip": true,
	// 浏览器环境头：不该透传给上游，避免触发上游 CSRF/安全校验
	"cookie":            true,
	"origin":            true,
	"referer":           true,
	"sec-fetch-site":    true,
	"sec-fetch-mode":    true,
	"sec-fetch-dest":    true,
	"sec-fetch-user":    true,
}

type relayRequest struct {
	c               *gin.Context
	inAdapter       model.Inbound
	internalRequest *model.InternalLLMRequest
	metrics         *RelayMetrics
	apiKeyID        int
	requestModel    string
	group           dbmodel.Group
	iter            *balancer.Iterator
	// 渠道测试只需要判断上游是否可用，不能把上游原始响应直接写给管理端接口。
	suppressResponse bool
	// 渠道测试不归属平台 API Key，避免写入 api_key_id=0 的会话保持记录。
	skipSticky bool
	// forceKeyID 用于渠道测试场景下强制锁定使用的 Key。
	// 当非 nil 时，executeRelay 会在拿到 candidate keys 后过滤为该 Key，并把 maxAttempts 强制为 1，
	// 即使渠道启用了 EnableMultiKeyRetry 也不会跨 Key 重试。
	// 注意：必须在 relay 层做此过滤而不是 handler 层——executeRelay 会通过 op.ChannelGet 从缓存
	// 重新加载完整 channel，handler 层对 channel.Keys 的修改会被该重新加载彻底覆盖。
	forceKeyID *int
}

// relayAttempt 尝试级上下文
type relayAttempt struct {
	*relayRequest // 嵌入请求级上下文

	outAdapter           model.Outbound
	channel              *dbmodel.Channel
	usedKey              dbmodel.ChannelKey
	firstTokenTimeOutSec int
}

// attemptResult 封装单次尝试的结果
type attemptResult struct {
	Success bool  // 是否成功
	Written bool  // 流式响应是否已开始写入（不可重试）
	Err     error // 失败时的错误
}
