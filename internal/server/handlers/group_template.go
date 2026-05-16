package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

// QuickGroupItem 快速创建分组的单个模型配置
type QuickGroupItem struct {
	ModelName  string   `json:"model_name" binding:"required"` // 模型名，同时作为分组名
	Mode       int      `json:"mode"`                          // 分组模式，默认1=轮询
	Exclude    []string `json:"exclude"`                       // 排除字段列表
	MatchRegex string   `json:"match_regex"`                   // 用户自定义正则（优先于自动生成）
}

// BatchCreateRequest 批量创建分组请求
type BatchCreateRequest struct {
	Groups []QuickGroupItem `json:"groups" binding:"required"`
}

// BatchCreateResult 单个分组的创建结果
type BatchCreateResult struct {
	ModelName  string `json:"model_name"`         // 模型名/分组名
	GroupID    int    `json:"group_id,omitempty"` // 创建成功的分组ID
	MatchCount int    `json:"match_count"`        // 匹配到的渠道模型数量
	Error      string `json:"error,omitempty"`    // 错误信息
}

func init() {
	router.NewGroupRouter("/api/v1/group-template").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/ungrouped-models", http.MethodGet).
				Handle(getUngroupedModels),
		).
		AddRoute(
			router.NewRoute("/batch-create", http.MethodPost).
				Handle(batchCreateGroups),
		)
}

// getUngroupedModels 获取未分组的模型列表
// 从所有渠道获取模型，排除已经在分组中的模型
func getUngroupedModels(c *gin.Context) {
	ctx := c.Request.Context()

	// 获取所有渠道的模型列表
	allModels, err := op.ChannelLLMList(ctx)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 收集已在分组中的 (ChannelID, ModelName) 集合
	grouped := make(map[string]bool)
	groups, err := op.GroupList(ctx)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	for _, group := range groups {
		for _, item := range group.Items {
			key := fmt.Sprintf("%d|%s", item.ChannelID, item.ModelName)
			grouped[key] = true
		}
	}

	// 过滤掉已分组的模型
	ungrouped := make([]model.LLMChannel, 0)
	for _, m := range allModels {
		key := fmt.Sprintf("%d|%s", m.ChannelID, m.Name)
		if !grouped[key] {
			ungrouped = append(ungrouped, m)
		}
	}

	resp.Success(c, ungrouped)
}

// batchCreateGroups 批量创建分组
// 对每个选中的模型：解析名称生成正则，创建分组，匹配渠道模型并添加
func batchCreateGroups(c *gin.Context) {
	var req BatchCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.Groups) == 0 {
		resp.Error(c, http.StatusBadRequest, "至少选择一个模型")
		return
	}

	ctx := c.Request.Context()

	// 获取所有渠道的模型列表，用于匹配
	allModels, err := op.ChannelLLMList(ctx)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	results := make([]BatchCreateResult, 0, len(req.Groups))

	for _, item := range req.Groups {
		result := BatchCreateResult{
			ModelName: item.ModelName,
		}

		// 确定分组模式
		mode := model.GroupMode(item.Mode)
		if mode < 1 || mode > 4 {
			mode = model.GroupModeRoundRobin
		}

		// 使用前端传入的正则（为空则使用 model_name 精确匹配）
		regex := strings.TrimSpace(item.MatchRegex)

		// 创建分组
		group := &model.Group{
			Name:       item.ModelName,
			Mode:       mode,
			MatchRegex: regex,
		}

		if err := op.GroupCreate(group, ctx); err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}

		// 正则为空时使用 model_name 模糊匹配（大小写不敏感包含，与手动添加分组行为一致）
		matched := make([]model.GroupIDAndLLMName, 0)
		if regex == "" {
			modelNameLower := strings.ToLower(item.ModelName)
			for _, m := range allModels {
				if strings.Contains(strings.ToLower(m.Name), modelNameLower) {
					matched = append(matched, model.GroupIDAndLLMName{
						ChannelID: m.ChannelID,
						ModelName: m.Name,
					})
				}
			}
		} else {
			// 用正则匹配渠道模型，收集匹配的 (ChannelID, ModelName)
			re, err := regexp.Compile(regex)
			if err != nil {
				result.Error = fmt.Sprintf("正则编译失败: %v", err)
				results = append(results, result)
				continue
			}
			for _, m := range allModels {
				if re.MatchString(m.Name) {
					matched = append(matched, model.GroupIDAndLLMName{
						ChannelID: m.ChannelID,
						ModelName: m.Name,
					})
				}
			}
		}

		result.MatchCount = len(matched)

		// 批量添加匹配的模型到分组
		if len(matched) > 0 {
			if err := op.GroupItemBatchAdd(group.ID, matched, ctx); err != nil {
				result.Error = fmt.Sprintf("添加模型失败: %v", err)
			}
		}

		result.GroupID = group.ID
		results = append(results, result)
	}

	resp.Success(c, results)
}
