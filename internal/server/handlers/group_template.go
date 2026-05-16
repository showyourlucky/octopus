package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/dlclark/regexp2"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

// QuickGroupItem 快速创建分组的单个模型配置
type QuickGroupItem struct {
	ModelName  string   `json:"model_name" binding:"required"` // 原始模型名（用于匹配渠道模型）
	GroupName  string   `json:"group_name"`                    // 自定义分组名（为空时使用 model_name）
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

// regexMaxLen 正则表达式最大长度限制，防止 ReDoS 攻击
const regexMaxLen = 512

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

// modelWithLower 携带预计算小写名的模型（避免循环内重复 ToLower）
type modelWithLower struct {
	model model.LLMChannel
	lower string
}

// batchCreateGroups 批量创建分组（事务保护）
// 第一阶段：验证请求、编译正则、匹配模型（事务外）
// 第二阶段：在单个事务中批量创建分组和模型项（任一失败则整体回滚）
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

	// 预计算所有模型名的小写版本，避免循环内重复调用 strings.ToLower
	modelsLower := make([]modelWithLower, len(allModels))
	for i, m := range allModels {
		modelsLower[i] = modelWithLower{model: m, lower: strings.ToLower(m.Name)}
	}

	// 第一阶段：验证请求项并匹配模型
	results := make([]BatchCreateResult, len(req.Groups))
	entries := make([]op.GroupWithItems, 0, len(req.Groups))
	entryIndices := make([]int, 0, len(req.Groups)) // entries[i] 对应 req.Groups[entryIndices[i]]

	for i, item := range req.Groups {
		results[i] = BatchCreateResult{ModelName: item.ModelName}

		// 确定分组模式
		mode := model.GroupMode(item.Mode)
		if mode < 1 || mode > 4 {
			mode = model.GroupModeRoundRobin
		}

		regex := strings.TrimSpace(item.MatchRegex)

		// ReDoS 防护：限制正则长度
		if len(regex) > regexMaxLen {
			results[i].Error = fmt.Sprintf("正则表达式过长（最大 %d 字符）", regexMaxLen)
			continue
		}

		// 分组名：优先使用自定义 group_name，否则使用 model_name
		groupName := strings.TrimSpace(item.GroupName)
		if groupName == "" {
			groupName = item.ModelName
		}

		group := model.Group{
			Name:       groupName,
			Mode:       mode,
			MatchRegex: regex,
		}

		// 匹配渠道模型
		var items []model.GroupItem
		if regex == "" {
			// 无正则时使用模型名模糊匹配（大小写不敏感包含）
			modelNameLower := strings.ToLower(item.ModelName)
			for _, ml := range modelsLower {
				if strings.Contains(ml.lower, modelNameLower) {
					items = append(items, model.GroupItem{
						ChannelID: ml.model.ChannelID,
						ModelName: ml.model.Name,
					})
				}
			}
		} else {
			// 编译正则（ECMAScript 模式，支持 (?i) 和负向前瞻等特性）
			re, err := regexp2.Compile(regex, regexp2.ECMAScript)
			if err != nil {
				results[i].Error = fmt.Sprintf("正则编译失败: %v", err)
				continue
			}
			for _, ml := range modelsLower {
				matched, err := re.MatchString(ml.model.Name)
				if err != nil {
					results[i].Error = fmt.Sprintf("正则匹配失败: %v", err)
					break
				}
				if matched {
					items = append(items, model.GroupItem{
						ChannelID: ml.model.ChannelID,
						ModelName: ml.model.Name,
					})
				}
			}
		}

		results[i].MatchCount = len(items)
		entries = append(entries, op.GroupWithItems{Group: group, Items: items})
		entryIndices = append(entryIndices, i)
	}

	// 第二阶段：在事务中批量创建
	if len(entries) > 0 {
		if err := op.GroupBatchCreate(entries, ctx); err != nil {
			// 事务整体失败，标记所有待创建项为失败
			for _, idx := range entryIndices {
				if results[idx].Error == "" {
					results[idx].Error = err.Error()
				}
			}
		} else {
			// 事务成功，填充 GroupID
			for j, entry := range entries {
				results[entryIndices[j]].GroupID = entry.Group.ID
			}
		}
	}

	resp.Success(c, results)
}
