package op

import (
	"context"
	"fmt"
	"sort"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

// APIKeyVisibleModels 返回 API Key 在 /v1/models 中可见的模型名称。
// group 类型保持旧行为：只返回分组名称；all_channel 类型额外返回所有渠道配置的模型名称。
func APIKeyVisibleModels(apiKey model.APIKey, ctx context.Context) ([]string, error) {
	groupModels, err := GroupListModel(ctx)
	if err != nil {
		return nil, err
	}

	modelSet := make(map[string]struct{}, len(groupModels))
	for _, name := range groupModels {
		if name == "" {
			continue
		}
		modelSet[name] = struct{}{}
	}

	if model.NormalizeAPIKeyModelAccessType(apiKey.ModelAccessType) == model.APIKeyModelAccessTypeAllChannel {
		channelModels, err := ChannelLLMList(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range channelModels {
			if item.Name == "" {
				continue
			}
			modelSet[item.Name] = struct{}{}
		}
	}

	models := make([]string, 0, len(modelSet))
	for name := range modelSet {
		models = append(models, name)
	}
	sort.Strings(models)
	return filterAPIKeySupportedModels(models, apiKey.SupportedModels), nil
}

// APIKeySupportsModel 检查 API Key 的 supported_models 白名单。
// 空白名单表示在当前访问类型范围内不限制模型。
func APIKeySupportsModel(apiKey model.APIKey, requestModel string) bool {
	if requestModel == "" {
		return false
	}
	allowed := xstrings.SplitTrimCompact(",", apiKey.SupportedModels)
	if len(allowed) == 0 {
		return true
	}
	for _, name := range allowed {
		if name == requestModel {
			return true
		}
	}
	return false
}

// APIKeyResolveGroup 解析请求模型对应的候选分组。
// 优先使用真实分组；只有 all_channel 类型才允许把分组外模型解析为临时精确匹配分组。
func APIKeyResolveGroup(apiKey model.APIKey, requestModel string, ctx context.Context) (model.Group, error) {
	group, err := GroupGetEnabledMap(requestModel, ctx)
	if err == nil {
		return group, nil
	}
	if model.NormalizeAPIKeyModelAccessType(apiKey.ModelAccessType) != model.APIKeyModelAccessTypeAllChannel {
		return model.Group{}, err
	}
	return buildExactChannelModelGroup(requestModel, ctx)
}

func filterAPIKeySupportedModels(models []string, supportedModels string) []string {
	allowed := xstrings.SplitTrimCompact(",", supportedModels)
	if len(allowed) == 0 {
		return models
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}
	filtered := make([]string, 0, len(models))
	for _, name := range models {
		if _, ok := allowedSet[name]; ok {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func buildExactChannelModelGroup(requestModel string, ctx context.Context) (model.Group, error) {
	channels, err := ChannelList(ctx)
	if err != nil {
		return model.Group{}, err
	}
	sort.Slice(channels, func(i, j int) bool {
		return channels[i].ID < channels[j].ID
	})

	items := make([]model.GroupItem, 0)
	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		if !channelHasExactModel(channel, requestModel) {
			continue
		}
		items = append(items, model.GroupItem{
			ChannelID: channel.ID,
			ModelName: requestModel,
			Priority:  len(items) + 1,
			Weight:    1,
		})
	}
	if len(items) == 0 {
		return model.Group{}, fmt.Errorf("model not found")
	}

	return model.Group{
		Name:                           requestModel,
		Mode:                           model.GroupModeFailover,
		Items:                          items,
		RuntimeFailoverOnRecentFailure: true,
	}, nil
}

func channelHasExactModel(channel model.Channel, requestModel string) bool {
	for _, name := range xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel) {
		if name == requestModel {
			return true
		}
	}
	return false
}
