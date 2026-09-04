package op

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/cache"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

var channelCache = cache.New[int, model.Channel](16)
var channelKeyCache = cache.New[int, model.ChannelKey](16)
var channelKeyCacheNeedUpdate = make(map[int]struct{})
var channelKeyCacheNeedUpdateLock sync.Mutex

// channelCacheMu 串行化 channelCache 的读-改-写（ChannelKeyUpdate / ChannelKeyEnableIfDue /
// ChannelEnabled / ChannelBaseUrlUpdate / channelRefreshCacheByID），避免并发更新互相覆盖
// （lost update）——例如扫描器自动启用 Key 的同时，relay 更新同渠道其它 Key 会把启用结果覆盖掉。
var channelCacheMu sync.Mutex
func ChannelList(ctx context.Context) ([]model.Channel, error) {
	channels := make([]model.Channel, 0, channelCache.Len())
	for _, channel := range channelCache.GetAll() {
		channels = append(channels, channel)
	}
	return channels, nil
}

func ChannelCreate(channel *model.Channel, ctx context.Context) error {
	keys := channel.Keys
	channel.Keys = nil
	channel.KeyLoadBalanceMode = model.NormalizeKeyLoadBalanceMode(channel.KeyLoadBalanceMode)
	channel.AutoEnableStrategy = model.NormalizeAutoEnableStrategy(channel.AutoEnableStrategy)
	channel.AutoEnableDailyTime = model.NormalizeAutoEnableDailyTime(channel.AutoEnableDailyTime)
	tx := db.GetDB().WithContext(ctx).Begin()
	if err := tx.Create(channel).Error; err != nil {
		tx.Rollback()
		return err
	}

	if len(keys) > 0 {
		for i := range keys {
			keys[i].ChannelID = channel.ID
		}
		if err := tx.CreateInBatches(keys, 50).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	if err := tx.Commit().Error; err != nil {
		return err
	}

	channel.Keys = keys
	channelCache.Set(channel.ID, *channel)
	for _, k := range channel.Keys {
		if k.ID != 0 {
			channelKeyCache.Set(k.ID, k)
		}
	}
	return nil
}

// ChannelKeyUpdate 仅更新 ChannelKey 的内存缓存（不落库），并标记为需要在 SaveCache 时写入数据库。
// channelCacheMu 保证读-改-写原子性：并发更新（relay 各 Key、自动启用扫描器）不会互相覆盖。
func ChannelKeyUpdate(key model.ChannelKey) error {
	if key.ID == 0 || key.ChannelID == 0 {
		return fmt.Errorf("invalid channel key")
	}
	channelCacheMu.Lock()
	defer channelCacheMu.Unlock()
	ch, ok := channelCache.Get(key.ChannelID)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	if len(ch.Keys) > 0 {
		keys := make([]model.ChannelKey, len(ch.Keys))
		copy(keys, ch.Keys)
		for i := range keys {
			if keys[i].ID == key.ID {
				keys[i] = key
				break
			}
		}
		ch.Keys = keys
	}
	channelCache.Set(key.ChannelID, ch)
	channelKeyCache.Set(key.ID, key)
	channelKeyCacheNeedUpdateLock.Lock()
	channelKeyCacheNeedUpdate[key.ID] = struct{}{}
	channelKeyCacheNeedUpdateLock.Unlock()
	return nil
}

// ChannelKeyEnableIfDue 自动启用入口：在 channelCacheMu 锁内读取该 Key 的最新状态，
// 仅当「当前仍为禁用」且「已到启用时刻（now-DisabledAt >= delaySec）」时才置为启用并写回。
// delaySec < 0 表示不做时长检查（daily 策略无最短门槛）。
// 返回是否实际启用了该 Key（false 表示已启用 / 未到期 / Key 已不存在）。
func ChannelKeyEnableIfDue(channelID, keyID int, now time.Time, delaySec int64) (bool, error) {
	channelCacheMu.Lock()
	defer channelCacheMu.Unlock()
	ch, ok := channelCache.Get(channelID)
	if !ok {
		return false, fmt.Errorf("channel not found")
	}
	keys := make([]model.ChannelKey, len(ch.Keys))
	copy(keys, ch.Keys)
	idx := -1
	for i := range keys {
		if keys[i].ID == keyID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, nil // Key 已从渠道移除
	}
	k := keys[idx]
	if k.Enabled {
		return false, nil // 已被并发扫描/手动启用，跳过
	}
	if delaySec >= 0 && now.Unix()-k.DisabledAt < delaySec {
		return false, nil // 未到启用时刻（以锁内最新 DisabledAt 为准，防止启用刚被再次禁用的 Key）
	}
	k.Enabled = true
	keys[idx] = k
	ch.Keys = keys
	channelCache.Set(channelID, ch)
	channelKeyCache.Set(keyID, k)
	channelKeyCacheNeedUpdateLock.Lock()
	channelKeyCacheNeedUpdate[keyID] = struct{}{}
	channelKeyCacheNeedUpdateLock.Unlock()
	return true, nil
}
func ChannelBaseUrlUpdate(channelID int, baseUrl []model.BaseUrl) error {
	channelCacheMu.Lock()
	defer channelCacheMu.Unlock()
	ch, ok := channelCache.Get(channelID)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	// Copy to decouple callers from internal cache storage.
	if baseUrl == nil {
		ch.BaseUrls = nil
	} else {
		cp := make([]model.BaseUrl, len(baseUrl))
		copy(cp, baseUrl)
		ch.BaseUrls = cp
	}
	channelCache.Set(channelID, ch)
	return nil
}

// ChannelKeySaveDB 将运行时更新过的 ChannelKey 缓存写入数据库。
func ChannelKeySaveDB(ctx context.Context) error {
	channelKeyCacheNeedUpdateLock.Lock()
	keyIDs := make([]int, 0, len(channelKeyCacheNeedUpdate))
	for id := range channelKeyCacheNeedUpdate {
		keyIDs = append(keyIDs, id)
	}
	channelKeyCacheNeedUpdate = make(map[int]struct{})
	channelKeyCacheNeedUpdateLock.Unlock()

	if len(keyIDs) == 0 {
		return nil
	}

	dbConn := db.GetDB().WithContext(ctx)
	for _, id := range keyIDs {
		k, ok := channelKeyCache.Get(id)
		if !ok {
			continue
		}
		if err := dbConn.Save(&k).Error; err != nil {
			return err
		}
	}
	return nil
}

func ChannelUpdate(req *model.ChannelUpdateRequest, ctx context.Context) (*model.Channel, error) {
	// 先落库运行时 Key 状态（自动禁用/自动启用/状态码等），避免随后的 channelRefreshCacheByID
	// 从旧 DB 重载时把这些内存态回滚（例如自动启用刚生效的 Key 被编辑操作重新置回禁用）。
	if err := ChannelKeySaveDB(ctx); err != nil {
		log.Warnf("failed to flush channel key state before channel update: %v", err)
	}
	oldChannel, ok := channelCache.Get(req.ID)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}

	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var selectFields []string
	updates := model.Channel{ID: req.ID}

	if req.Name != nil {
		selectFields = append(selectFields, "name")
		updates.Name = *req.Name
	}
	if req.Type != nil {
		selectFields = append(selectFields, "type")
		updates.Type = *req.Type
	}
	if req.Enabled != nil {
		selectFields = append(selectFields, "enabled")
		updates.Enabled = *req.Enabled
	}
	if req.BaseUrls != nil {
		selectFields = append(selectFields, "base_urls")
		updates.BaseUrls = *req.BaseUrls
	}
	if req.Model != nil {
		selectFields = append(selectFields, "model")
		updates.Model = *req.Model
	}
	if req.CustomModel != nil {
		selectFields = append(selectFields, "custom_model")
		updates.CustomModel = *req.CustomModel
	}
	if req.Proxy != nil {
		selectFields = append(selectFields, "proxy")
		updates.Proxy = *req.Proxy
	}
	if req.AutoSync != nil {
		selectFields = append(selectFields, "auto_sync")
		updates.AutoSync = *req.AutoSync
	}
	if req.AutoGroup != nil {
		selectFields = append(selectFields, "auto_group")
		updates.AutoGroup = *req.AutoGroup
	}
	if req.CustomHeader != nil {
		selectFields = append(selectFields, "custom_header")
		updates.CustomHeader = *req.CustomHeader
	}
	if req.ChannelProxy != nil {
		selectFields = append(selectFields, "channel_proxy")
		updates.ChannelProxy = req.ChannelProxy
	}
	if req.ParamOverride != nil {
		selectFields = append(selectFields, "param_override")
		updates.ParamOverride = req.ParamOverride
	}
	if req.MatchRegex != nil {
		selectFields = append(selectFields, "match_regex")
		updates.MatchRegex = req.MatchRegex
	}
	if req.EnableMultiKeyRetry != nil {
		selectFields = append(selectFields, "enable_multi_key_retry")
		updates.EnableMultiKeyRetry = *req.EnableMultiKeyRetry
	}
	if req.RetryCount != nil {
		selectFields = append(selectFields, "retry_count")
		updates.RetryCount = *req.RetryCount
	}
	if req.KeyLoadBalanceMode != nil {
		selectFields = append(selectFields, "key_load_balance_mode")
		updates.KeyLoadBalanceMode = model.NormalizeKeyLoadBalanceMode(*req.KeyLoadBalanceMode)
	}
	if req.AutoBanKeyFailures != nil {
		selectFields = append(selectFields, "auto_ban_key_failures")
		updates.AutoBanKeyFailures = *req.AutoBanKeyFailures
	}
	if req.AutoEnableKey != nil {
		selectFields = append(selectFields, "auto_enable_key")
		updates.AutoEnableKey = *req.AutoEnableKey
	}
	if req.AutoEnableStrategy != nil {
		selectFields = append(selectFields, "auto_enable_strategy")
		updates.AutoEnableStrategy = model.NormalizeAutoEnableStrategy(*req.AutoEnableStrategy)
	}
	if req.AutoEnableDelayMin != nil {
		selectFields = append(selectFields, "auto_enable_delay_min")
		updates.AutoEnableDelayMin = *req.AutoEnableDelayMin
	}
	if req.AutoEnableBackoffMaxMin != nil {
		selectFields = append(selectFields, "auto_enable_backoff_max_min")
		updates.AutoEnableBackoffMaxMin = *req.AutoEnableBackoffMaxMin
	}
	if req.AutoEnableDailyTime != nil {
		selectFields = append(selectFields, "auto_enable_daily_time")
		updates.AutoEnableDailyTime = model.NormalizeAutoEnableDailyTime(*req.AutoEnableDailyTime)
	}

	// 只有当有字段需要更新时才执行 UPDATE
	if len(selectFields) > 0 {
		if err := tx.Model(&model.Channel{}).Where("id = ?", req.ID).Select(selectFields).Updates(&updates).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to update channel: %w", err)
		}
	}

	// 删除 keys
	if len(req.KeysToDelete) > 0 {
		batchSize := 50
		for i := 0; i < len(req.KeysToDelete); i += batchSize {
			end := i + batchSize
			if end > len(req.KeysToDelete) {
				end = len(req.KeysToDelete)
			}
			batch := req.KeysToDelete[i:end]
			if err := tx.Where("id IN ? AND channel_id = ?", batch, req.ID).Delete(&model.ChannelKey{}).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to delete channel keys: %w", err)
			}
		}
	}

	// 更新 keys（逐条，只更新提供的字段）
	if len(req.KeysToUpdate) > 0 {
		for _, ku := range req.KeysToUpdate {
			updates := map[string]interface{}{}
			if ku.Enabled != nil {
				updates["enabled"] = *ku.Enabled
				// 手动改 Key 状态（Enabled 非 nil）→ AutoBanCount 清零，退出自动启用候选
				updates["auto_ban_count"] = 0
			}
			if ku.ChannelKey != nil {
				updates["channel_key"] = *ku.ChannelKey
			}
			if ku.Remark != nil {
				updates["remark"] = *ku.Remark
			}
			if len(updates) == 0 {
				continue
			}
			if err := tx.Model(&model.ChannelKey{}).
				Where("id = ? AND channel_id = ?", ku.ID, req.ID).
				Updates(updates).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to update channel key %d: %w", ku.ID, err)
			}
		}
	}

	// 新增 keys
	if len(req.KeysToAdd) > 0 {
		newKeys := make([]model.ChannelKey, 0, len(req.KeysToAdd))
		for _, ka := range req.KeysToAdd {
			newKeys = append(newKeys, model.ChannelKey{
				ChannelID:  req.ID,
				Enabled:    ka.Enabled,
				ChannelKey: ka.ChannelKey,
				Remark:     ka.Remark,
			})
		}
		if err := tx.Create(&newKeys).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create channel keys: %w", err)
		}
	}

	// 渠道开关 false→true：重置该渠道所有自动禁用 Key 的 DisabledAt（计时重新开始，不补启用）
	if req.Enabled != nil && *req.Enabled && !oldChannel.Enabled {
		if err := tx.Model(&model.ChannelKey{}).
			Where("channel_id = ? AND auto_ban_count > 0 AND enabled = ?", req.ID, false).
			Update("disabled_at", time.Now().Unix()).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to reset channel key disabled_at: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 刷新缓存并返回最新数据
	if err := channelRefreshCacheByID(req.ID, ctx); err != nil {
		return nil, err
	}

	channel, _ := channelCache.Get(req.ID)
	return &channel, nil
}

func ChannelEnabled(id int, enabled bool, ctx context.Context) error {
	// 先落库运行时 Key 状态，避免随后的缓存重载把自动禁用/自动启用等内存态回滚。
	if err := ChannelKeySaveDB(ctx); err != nil {
		log.Warnf("failed to flush channel key state before channel enabled update: %v", err)
	}
	oldChannel, ok := channelCache.Get(id)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	if err := db.GetDB().WithContext(ctx).Model(&model.Channel{}).Where("id = ?", id).Update("enabled", enabled).Error; err != nil {
		return err
	}
	// 渠道开关 false→true：重置该渠道所有自动禁用 Key 的 DisabledAt（计时重新开始，不补启用），
	// 并从 DB 重新加载该渠道（含 Keys）以同步缓存。
	if enabled && !oldChannel.Enabled {
		if err := db.GetDB().WithContext(ctx).Model(&model.ChannelKey{}).
			Where("channel_id = ? AND auto_ban_count > 0 AND enabled = ?", id, false).
			Update("disabled_at", time.Now().Unix()).Error; err != nil {
			return err
		}
		return channelRefreshCacheByID(id, ctx)
	}
	// 非翻转路径：在锁内重新读取最新 channel 再合并写回，避免与并发的 ChannelKeyUpdate 互相覆盖。
	channelCacheMu.Lock()
	oldChannel, _ = channelCache.Get(id)
	oldChannel.Enabled = enabled
	channelCache.Set(id, oldChannel)
	channelCacheMu.Unlock()
	return nil
}

func ChannelDel(id int, ctx context.Context) error {
	ch, ok := channelCache.Get(id)
	if !ok {
		return fmt.Errorf("channel not found")
	}

	// 开启事务
	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 获取所有受影响的 GroupID，用于刷新缓存
	var affectedGroupIDs []int
	if err := tx.Model(&model.GroupItem{}).
		Where("channel_id = ?", id).
		Pluck("group_id", &affectedGroupIDs).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to get affected groups: %w", err)
	}

	// 删除所有引用该渠道的 GroupItem
	if err := tx.Where("channel_id = ?", id).Delete(&model.GroupItem{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete group items: %w", err)
	}

	// 删除渠道 keys
	if err := tx.Where("channel_id = ?", id).Delete(&model.ChannelKey{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete channel keys: %w", err)
	}

	// 删除统计数据
	if err := tx.Where("channel_id = ?", id).Delete(&model.StatsChannel{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete channel stats: %w", err)
	}

	// 删除渠道
	if err := tx.Delete(&model.Channel{}, id).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete channel: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 删除缓存
	channelCache.Del(id)
	for _, k := range ch.Keys {
		if k.ID != 0 {
			channelKeyCache.Del(k.ID)
		}
	}
	StatsChannelDel(id)

	// 刷新受影响的分组缓存
	for _, groupID := range affectedGroupIDs {
		if err := groupRefreshCacheByID(groupID, ctx); err != nil {
			log.Warnf("failed to refresh group cache for group %d: %v", groupID, err)
		}
	}

	return nil
}

func ChannelLLMList(ctx context.Context) ([]model.LLMChannel, error) {
	models := []model.LLMChannel{}
	for _, channel := range channelCache.GetAll() {
		modelNames := xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel)
		for _, modelName := range modelNames {
			if modelName == "" {
				continue
			}
			models = append(models, model.LLMChannel{
				Name:        modelName,
				Enabled:     channel.Enabled,
				ChannelID:   channel.ID,
				ChannelName: channel.Name,
			})
		}
	}
	return models, nil
}

func ChannelGet(id int, ctx context.Context) (*model.Channel, error) {
	channel, ok := channelCache.Get(id)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	return &channel, nil
}

func channelRefreshCache(ctx context.Context) error {
	channelCacheMu.Lock()
	defer channelCacheMu.Unlock()
	channels := []model.Channel{}
	if err := db.GetDB().WithContext(ctx).
		Preload("Keys").
		Preload("Stats").
		Find(&channels).Error; err != nil {
		log.Warnf("failed to get channels: %v", err)
		return err
	}
	channelKeyCache.Clear()
	channelKeyCacheNeedUpdateLock.Lock()
	channelKeyCacheNeedUpdate = make(map[int]struct{})
	channelKeyCacheNeedUpdateLock.Unlock()
	for _, channel := range channels {
		channelCache.Set(channel.ID, channel)
		for _, k := range channel.Keys {
			if k.ID != 0 {
				channelKeyCache.Set(k.ID, k)
			}
		}
	}
	return nil
}

func channelRefreshCacheByID(id int, ctx context.Context) error {
	channelCacheMu.Lock()
	defer channelCacheMu.Unlock()
	if old, ok := channelCache.Get(id); ok {
		for _, k := range old.Keys {
			if k.ID != 0 {
				channelKeyCache.Del(k.ID)
			}
		}
	}
	var channel model.Channel
	if err := db.GetDB().WithContext(ctx).
		Preload("Keys").
		Preload("Stats").
		First(&channel, id).Error; err != nil {
		return err
	}
	channelCache.Set(channel.ID, channel)
	for _, k := range channel.Keys {
		if k.ID != 0 {
			channelKeyCache.Set(k.ID, k)
		}
	}
	return nil
}
