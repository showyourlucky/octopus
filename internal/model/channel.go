package model

import (
	"math/rand/v2"
	"sort"
	"time"
)

// OutboundType 渠道出站协议类型。
// 原先定义在 internal/transformer/outbound，axonhub/llm 迁移后搬到 model 包，
// transformer 的具体实现由 internal/relay/llm.go 按此类型选用 axonhub 的 transformer。
type OutboundType int

const (
	OutboundTypeOpenAIChat OutboundType = iota
	OutboundTypeOpenAIResponse
	OutboundTypeAnthropic
	OutboundTypeGemini
	OutboundTypeVolcengine
	OutboundTypeOpenAIEmbedding
)

// EmbeddingChannelTypes 定义支持 embedding 请求的 channel 类型集合
var EmbeddingChannelTypes = map[OutboundType]bool{
	OutboundTypeOpenAIEmbedding: true,
}

// ChatChannelTypes 定义支持 chat 请求的 channel 类型集合
var ChatChannelTypes = map[OutboundType]bool{
	OutboundTypeOpenAIChat:     true,
	OutboundTypeOpenAIResponse: true,
	OutboundTypeAnthropic:      true,
	OutboundTypeGemini:         true,
	OutboundTypeVolcengine:      true,
}

// IsEmbeddingChannelType 判断 channel 类型是否支持 embedding 请求
func IsEmbeddingChannelType(channelType OutboundType) bool {
	return EmbeddingChannelTypes[channelType]
}

// IsChatChannelType 判断 channel 类型是否支持 chat 请求
func IsChatChannelType(channelType OutboundType) bool {
	return ChatChannelTypes[channelType]
}

type AutoGroupType int

const (
	AutoGroupTypeNone  AutoGroupType = 0 //不自动分组
	AutoGroupTypeFuzzy AutoGroupType = 1 //模糊匹配
	AutoGroupTypeExact AutoGroupType = 2 //准确匹配
	AutoGroupTypeRegex AutoGroupType = 3 //正则匹配
)

const (
	KeyLoadBalanceModeFailover   = "failover"
	KeyLoadBalanceModeRoundRobin = "round_robin"
	KeyLoadBalanceModeRandom     = "random"
)

type Channel struct {
	ID                  int                   `json:"id" gorm:"primaryKey"`
	Name                string                `json:"name" gorm:"unique;not null"`
	Type                OutboundType `json:"type"`
	Enabled             bool                  `json:"enabled" gorm:"default:true"`
	BaseUrls            []BaseUrl             `json:"base_urls" gorm:"serializer:json"`
	Keys                []ChannelKey          `json:"keys" gorm:"foreignKey:ChannelID"`
	Model               string                `json:"model"`
	CustomModel         string                `json:"custom_model"`
	Proxy               bool                  `json:"proxy" gorm:"default:false"`
	AutoSync            bool                  `json:"auto_sync" gorm:"default:false"`
	AutoGroup           AutoGroupType         `json:"auto_group" gorm:"default:0"`
	CustomHeader        []CustomHeader        `json:"custom_header" gorm:"serializer:json"`
	ParamOverride       *string               `json:"param_override"`
	ChannelProxy        *string               `json:"channel_proxy"`
	Stats               *StatsChannel         `json:"stats,omitempty" gorm:"foreignKey:ChannelID"`
	MatchRegex          *string               `json:"match_regex"`
	EnableMultiKeyRetry bool                  `json:"enable_multi_key_retry" gorm:"default:false"`
	RetryCount          int                   `json:"retry_count" gorm:"default:3"`
	KeyLoadBalanceMode  string                `json:"key_load_balance_mode" gorm:"default:'failover'"`
	AutoBanKeyFailures  int                   `json:"auto_ban_key_failures" gorm:"default:0"` // 0 means disabled
	// 自动启用已禁用 Key 配置
	AutoEnableKey           bool   `json:"auto_enable_key" gorm:"default:false"`
	AutoEnableStrategy      string `json:"auto_enable_strategy" gorm:"default:'fixed'"`      // fixed / daily / backoff
	AutoEnableDelayMin      int    `json:"auto_enable_delay_min" gorm:"default:1440"`        // 固定延迟/退避基础延迟（分钟）
	AutoEnableBackoffMaxMin int    `json:"auto_enable_backoff_max_min" gorm:"default:43200"` // 退避上限（分钟）
	AutoEnableDailyTime     string `json:"auto_enable_daily_time" gorm:"default:'00:00'"`    // 每日定点 HH:MM（00:00–23:59）
}

type BaseUrl struct {
	URL   string `json:"url"`
	Delay int    `json:"delay"`
}

type CustomHeader struct {
	HeaderKey   string `json:"header_key"`
	HeaderValue string `json:"header_value"`
}

type ChannelKey struct {
	ID               int     `json:"id" gorm:"primaryKey"`
	ChannelID        int     `json:"channel_id"`
	Enabled          bool    `json:"enabled" gorm:"default:true"`
	ChannelKey       string  `json:"channel_key"`
	StatusCode       int     `json:"status_code"`
	LastUseTimeStamp int64   `json:"last_use_time_stamp"`
	TotalCost        float64 `json:"total_cost"`
	Remark           string  `json:"remark"`
	// 自动禁用计数（自动禁用时 +1，手动改 Key 状态时清零）
	AutoBanCount int   `json:"auto_ban_count"` // 存量行默认 0，不参与扫描
	DisabledAt   int64 `json:"disabled_at"`    // 最近一次自动禁用时刻（unix 秒）
}

// ChannelUpdateRequest 渠道更新请求 - 仅包含变更的数据
type ChannelUpdateRequest struct {
	ID            int                    `json:"id" binding:"required"`
	Name          *string                `json:"name,omitempty"`
	Type          *OutboundType `json:"type,omitempty"`
	Enabled       *bool                  `json:"enabled,omitempty"`
	BaseUrls      *[]BaseUrl             `json:"base_urls,omitempty"`
	Model         *string                `json:"model,omitempty"`
	CustomModel   *string                `json:"custom_model,omitempty"`
	Proxy         *bool                  `json:"proxy,omitempty"`
	AutoSync      *bool                  `json:"auto_sync,omitempty"`
	AutoGroup     *AutoGroupType         `json:"auto_group,omitempty"`
	CustomHeader  *[]CustomHeader        `json:"custom_header,omitempty"`
	ChannelProxy  *string                `json:"channel_proxy,omitempty"`
	ParamOverride *string                `json:"param_override,omitempty"`
	MatchRegex    *string                `json:"match_regex,omitempty"`

	EnableMultiKeyRetry *bool   `json:"enable_multi_key_retry,omitempty"`
	RetryCount          *int    `json:"retry_count,omitempty"`
	KeyLoadBalanceMode  *string `json:"key_load_balance_mode,omitempty"`
	AutoBanKeyFailures  *int    `json:"auto_ban_key_failures,omitempty"`

	AutoEnableKey           *bool   `json:"auto_enable_key,omitempty"`
	AutoEnableStrategy      *string `json:"auto_enable_strategy,omitempty"`
	AutoEnableDelayMin      *int    `json:"auto_enable_delay_min,omitempty"`
	AutoEnableBackoffMaxMin *int    `json:"auto_enable_backoff_max_min,omitempty"`
	AutoEnableDailyTime     *string `json:"auto_enable_daily_time,omitempty"`

	KeysToAdd    []ChannelKeyAddRequest    `json:"keys_to_add,omitempty"`
	KeysToUpdate []ChannelKeyUpdateRequest `json:"keys_to_update,omitempty"`
	KeysToDelete []int                     `json:"keys_to_delete,omitempty"`
}

type ChannelKeyAddRequest struct {
	Enabled    bool   `json:"enabled"`
	ChannelKey string `json:"channel_key" binding:"required"`
	Remark     string `json:"remark"`
}

type ChannelKeyUpdateRequest struct {
	ID         int     `json:"id" binding:"required"`
	Enabled    *bool   `json:"enabled,omitempty"`
	ChannelKey *string `json:"channel_key,omitempty"`
	Remark     *string `json:"remark,omitempty"`
}

// ChannelFetchModelRequest is used by /channel/fetch-model (not persisted).
type ChannelFetchModelRequest struct {
	Type    OutboundType `json:"type" binding:"required"`
	BaseURL string                `json:"base_url" binding:"required"`
	Key     string                `json:"key" binding:"required"`
	Proxy   bool                  `json:"proxy"`
}

func (c *Channel) GetBaseUrl() string {
	if c == nil || len(c.BaseUrls) == 0 {
		return ""
	}

	bestURL := ""
	bestDelay := 0
	bestSet := false

	for _, bu := range c.BaseUrls {
		if bu.URL == "" {
			continue
		}
		if !bestSet || bu.Delay < bestDelay {
			bestURL = bu.URL
			bestDelay = bu.Delay
			bestSet = true
		}
	}

	return bestURL
}

func (c *Channel) GetChannelKey() ChannelKey {
	if c == nil || len(c.Keys) == 0 {
		return ChannelKey{}
	}

	nowSec := time.Now().Unix()

	best := ChannelKey{}
	bestCost := 0.0
	bestSet := false

	for _, k := range c.Keys {
		if !k.Enabled || k.ChannelKey == "" {
			continue
		}
		if k.StatusCode == 429 && k.LastUseTimeStamp > 0 {
			if nowSec-k.LastUseTimeStamp < int64(5*time.Minute/time.Second) {
				continue
			}
		}
		if !bestSet || k.TotalCost < bestCost {
			best = k
			bestCost = k.TotalCost
			bestSet = true
		}
	}

	if !bestSet {
		return ChannelKey{}
	}
	return best
}

// NormalizeKeyLoadBalanceMode 归一化 Key 负载方式，未知/空值回退 failover。
func NormalizeKeyLoadBalanceMode(mode string) string {
	switch mode {
	case KeyLoadBalanceModeFailover, KeyLoadBalanceModeRoundRobin, KeyLoadBalanceModeRandom:
		return mode
	default:
		return KeyLoadBalanceModeFailover
	}
}

// 自动启用策略常量
const (
	AutoEnableStrategyFixed   = "fixed"
	AutoEnableStrategyDaily   = "daily"
	AutoEnableStrategyBackoff = "backoff"
)

// NormalizeAutoEnableStrategy 归一化自动启用策略，未知/空值回退 fixed。
func NormalizeAutoEnableStrategy(s string) string {
	switch s {
	case AutoEnableStrategyDaily, AutoEnableStrategyBackoff:
		return s
	default:
		return AutoEnableStrategyFixed
	}
}

// NormalizeAutoEnableDailyTime 归一化每日定点时刻：UI 语义 "24:00"（凌晨24点）存储转 "00:00"。
func NormalizeAutoEnableDailyTime(s string) string {
	if s == "24:00" {
		return "00:00"
	}
	return s
}

func (c *Channel) GetCandidateKeys() []ChannelKey {
	if c == nil || len(c.Keys) == 0 {
		return nil
	}

	nowSec := time.Now().Unix()
	var candidates []ChannelKey

	for _, k := range c.Keys {
		if !k.Enabled || k.ChannelKey == "" {
			continue
		}
		if k.StatusCode == 429 && k.LastUseTimeStamp > 0 {
			if nowSec-k.LastUseTimeStamp < int64(5*time.Minute/time.Second) {
				continue
			}
		}
		candidates = append(candidates, k)
	}

	if len(candidates) == 0 {
		return nil
	}

	switch NormalizeKeyLoadBalanceMode(c.KeyLoadBalanceMode) {
	case KeyLoadBalanceModeRandom:
		rand.Shuffle(len(candidates), func(i, j int) {
			candidates[i], candidates[j] = candidates[j], candidates[i]
		})
	case KeyLoadBalanceModeRoundRobin:
		// 轮询：优先选择最久未使用的 Key；时间相同时用 ID 保证排序稳定。
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].LastUseTimeStamp == candidates[j].LastUseTimeStamp {
				return candidates[i].ID < candidates[j].ID
			}
			return candidates[i].LastUseTimeStamp < candidates[j].LastUseTimeStamp
		})
	default:
		// 故障转移：固定使用排序最靠前的可用 Key，只有失败重试时才切换到后续 Key。
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].ID < candidates[j].ID
		})
	}

	return candidates
}
