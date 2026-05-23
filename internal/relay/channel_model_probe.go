package relay

import (
	"fmt"
	"strings"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/gin-gonic/gin"
)

const channelModelProbeAPIKeyName = "渠道测试"

// ChannelModelProbeResult 是管理端渠道模型测试的摘要结果。
// 真实上游响应仍会进入现有日志，管理端接口只返回诊断需要的最小信息。
type ChannelModelProbeResult struct {
	Success    bool   `json:"success"`
	Model      string `json:"model"`
	DurationMs int64  `json:"duration_ms"`
	Message    string `json:"message"`
}

// ProbeChannelModel 固定在指定渠道上发起一次真实非流式 Chat 请求。
// 这里复用 relayAttempt 的转发、Key 选择、熔断、统计和日志逻辑，只把候选分组收敛为单个渠道。
func ProbeChannelModel(c *gin.Context, channel dbmodel.Channel, modelName string) (*ChannelModelProbeResult, error) {
	content := "1+78=?"
	stream := false
	internalRequest := &transformerModel.InternalLLMRequest{
		Model:  modelName,
		Stream: &stream,
		Messages: []transformerModel.Message{
			{
				Role: "user",
				Content: transformerModel.MessageContent{
					Content: &content,
				},
			},
		},
	}
	if err := internalRequest.Validate(); err != nil {
		return nil, fmt.Errorf("测试请求构造失败: %w", err)
	}

	inAdapter := inbound.Get(inbound.InboundTypeOpenAIChat)
	if inAdapter == nil {
		return nil, fmt.Errorf("Chat 入站适配器不可用")
	}

	group := dbmodel.Group{
		Mode: dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{
			{
				ChannelID: channel.ID,
				ModelName: modelName,
			},
		},
	}
	iter := balancer.NewIterator(group, 0, modelName)
	metrics := NewRelayMetrics(0, modelName, internalRequest)
	metrics.SkipAPIKeyStats = true
	metrics.RequestAPIKeyName = channelModelProbeAPIKeyName

	start := time.Now()
	req := &relayRequest{
		c:                c,
		inAdapter:        inAdapter,
		internalRequest:  internalRequest,
		metrics:          metrics,
		requestModel:     modelName,
		group:            group,
		iter:             iter,
		suppressResponse: true,
		skipSticky:       true,
	}

	result := executeRelay(req)
	durationMs := time.Since(start).Milliseconds()
	if result.Success {
		return &ChannelModelProbeResult{
			Success:    true,
			Model:      modelName,
			DurationMs: durationMs,
			Message:    "渠道模型测试成功",
		}, nil
	}
	if message := channelProbeFailureMessage(result.Err, iter.Attempts()); message != "" {
		return nil, fmt.Errorf("渠道模型测试失败: %s", message)
	}
	return nil, fmt.Errorf("渠道模型测试失败")
}

func channelProbeFailureMessage(err error, attempts []dbmodel.ChannelAttempt) string {
	for i := len(attempts) - 1; i >= 0; i-- {
		msg := strings.TrimSpace(attempts[i].Msg)
		if msg != "" {
			return msg
		}
	}
	if err != nil {
		return err.Error()
	}
	return ""
}
