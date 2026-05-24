package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/task"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/channel").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listChannel),
		).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Handle(createChannel),
		).
		AddRoute(
			router.NewRoute("/update", http.MethodPost).
				Handle(updateChannel),
		).
		AddRoute(
			router.NewRoute("/enable", http.MethodPost).
				Handle(enableChannel),
		).
		AddRoute(
			router.NewRoute("/delete/:id", http.MethodDelete).
				Handle(deleteChannel),
		).
		AddRoute(
			router.NewRoute("/fetch-model", http.MethodPost).
				Handle(fetchModel),
		).
		AddRoute(
			router.NewRoute("/test-model", http.MethodPost).
				Handle(testChannelModel),
		)
	router.NewGroupRouter("/api/v1/channel").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/sync", http.MethodPost).
				Handle(syncChannel),
		).
		AddRoute(
			router.NewRoute("/last-sync-time", http.MethodGet).
				Handle(getLastSyncTime),
		)
}

func listChannel(c *gin.Context) {
	channels, err := op.ChannelList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	for i, channel := range channels {
		stats := op.StatsChannelGet(channel.ID)
		channels[i].Stats = &stats
	}
	resp.Success(c, channels)
}

func createChannel(c *gin.Context) {
	var channel model.Channel
	if err := c.ShouldBindJSON(&channel); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := op.ChannelCreate(&channel, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	stats := op.StatsChannelGet(channel.ID)
	channel.Stats = &stats
	go func(channel *model.Channel) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		modelStr := channel.Model + "," + channel.CustomModel
		modelArray := strings.Split(modelStr, ",")
		helper.LLMPriceAddToDB(modelArray, ctx)
		helper.ChannelBaseUrlDelayUpdate(channel, ctx)
		helper.ChannelAutoGroup(channel, ctx)
	}(&channel)
	resp.Success(c, channel)
}

func updateChannel(c *gin.Context) {
	var req model.ChannelUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	channel, err := op.ChannelUpdate(&req, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	stats := op.StatsChannelGet(channel.ID)
	channel.Stats = &stats
	go func(channel *model.Channel) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		modelStr := channel.Model + "," + channel.CustomModel
		modelArray := strings.Split(modelStr, ",")
		helper.LLMPriceAddToDB(modelArray, ctx)
		helper.ChannelBaseUrlDelayUpdate(channel, ctx)
		helper.ChannelAutoGroup(channel, ctx)
	}(channel)
	resp.Success(c, channel)
}

func enableChannel(c *gin.Context) {
	var request struct {
		ID      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := op.ChannelEnabled(request.ID, request.Enabled, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func deleteChannel(c *gin.Context) {
	id := c.Param("id")
	idNum, err := strconv.Atoi(id)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	if err := op.ChannelDel(idNum, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}
func fetchModel(c *gin.Context) {
	var request model.Channel
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	models, err := helper.FetchModels(c.Request.Context(), request)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, models)
}

func testChannelModel(c *gin.Context) {
	var request struct {
		ChannelID int    `json:"channel_id" binding:"required"`
		Model     string `json:"model" binding:"required"`
		// KeyID 可选：指定测试时使用的 Key ID；未传时由后端按渠道默认负载策略选择。
		// 使用 *int 区分“未指定”与“显式 0”，避免误把零值视为有效 ID。
		KeyID *int `json:"key_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	request.Model = strings.TrimSpace(request.Model)
	if request.Model == "" {
		resp.Error(c, http.StatusBadRequest, "模型不能为空")
		return
	}

	channel, err := op.ChannelGet(request.ChannelID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, "渠道不存在")
		return
	}
	if !channel.Enabled {
		resp.Error(c, http.StatusBadRequest, "渠道未启用，无法发起测试")
		return
	}
	if !outbound.IsChatChannelType(channel.Type) {
		resp.Error(c, http.StatusBadRequest, "当前第一版仅支持 Chat 类渠道测试")
		return
	}
	if !channelContainsModel(channel, request.Model) {
		resp.Error(c, http.StatusBadRequest, "模型不属于当前渠道")
		return
	}
	if len(channel.GetCandidateKeys()) == 0 {
		resp.Error(c, http.StatusBadRequest, "当前渠道没有可用 Key")
		return
	}

	// 若指定了 KeyID，先在 handler 层做一次存在性 + 启用状态预校验，
	// 让"指定 Key 不存在/已禁用"能立即返回友好提示，而不是落到 relay 内部当作 skip 处理。
	// 真正的"锁定到该 Key + 禁止多 Key 重试"动作下沉到 relay/executeRelay，
	// 因为 executeRelay 会通过 op.ChannelGet 重新加载完整 channel，handler 层修改 channel.Keys 无效。
	if request.KeyID != nil {
		var matched bool
		for i := range channel.Keys {
			if channel.Keys[i].ID == *request.KeyID && channel.Keys[i].Enabled {
				matched = true
				break
			}
		}
		if !matched {
			resp.Error(c, http.StatusBadRequest, "指定的 Key 不存在或已禁用")
			return
		}
	}

	result, err := relay.ProbeChannelModel(c, *channel, request.Model, request.KeyID)
	if err != nil {
		resp.Error(c, http.StatusBadGateway, err.Error())
		return
	}
	resp.Success(c, result)
}

func channelContainsModel(channel *model.Channel, modelName string) bool {
	if channel == nil {
		return false
	}
	models := strings.Split(channel.Model+","+channel.CustomModel, ",")
	for _, item := range models {
		if strings.TrimSpace(item) == modelName {
			return true
		}
	}
	return false
}

func syncChannel(c *gin.Context) {
	task.SyncModelsTask()
	resp.Success(c, nil)
}

func getLastSyncTime(c *gin.Context) {
	time := task.GetLastSyncModelsTime()
	resp.Success(c, time)
}
