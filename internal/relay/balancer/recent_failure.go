package balancer

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

type recentFailureEntry struct {
	failedAt time.Time
}

var recentFailures sync.Map // key: channelID:modelName -> recentFailureEntry

func recentFailureKey(channelID int, modelName string) string {
	return fmt.Sprintf("%d:%s", channelID, modelName)
}

// RecordRecentModelFailure 记录分组外精确模型的一次近期失败。
// 该状态只影响后续候选排序，不直接禁用渠道，也不替代熔断器。
func RecordRecentModelFailure(channelID int, modelName string) {
	if channelID == 0 || modelName == "" {
		return
	}
	recentFailures.Store(recentFailureKey(channelID, modelName), recentFailureEntry{failedAt: time.Now()})
}

// ClearRecentModelFailure 在渠道恢复成功后清理近期失败状态。
func ClearRecentModelFailure(channelID int, modelName string) {
	if channelID == 0 || modelName == "" {
		return
	}
	recentFailures.Delete(recentFailureKey(channelID, modelName))
}

func isRecentlyFailed(channelID int, modelName string, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	v, ok := recentFailures.Load(recentFailureKey(channelID, modelName))
	if !ok {
		return false
	}
	entry, ok := v.(recentFailureEntry)
	if !ok {
		recentFailures.Delete(recentFailureKey(channelID, modelName))
		return false
	}
	if time.Since(entry.failedAt) >= ttl {
		recentFailures.Delete(recentFailureKey(channelID, modelName))
		return false
	}
	return true
}

func sortRecentFailuresLast(items []model.GroupItem, modelName string) []model.GroupItem {
	if len(items) == 0 {
		return nil
	}
	ttl := GetCooldown(1)
	sorted := make([]model.GroupItem, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		leftFailed := isRecentlyFailed(sorted[i].ChannelID, modelName, ttl)
		rightFailed := isRecentlyFailed(sorted[j].ChannelID, modelName, ttl)
		if leftFailed == rightFailed {
			return sorted[i].Priority < sorted[j].Priority
		}
		return !leftFailed && rightFailed
	})
	return sorted
}
