package task

import (
	"context"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// TaskAutoEnableKey 自动启用已禁用 Key 扫描任务（5 分钟 tick，启动即跑一次）。
const TaskAutoEnableKey = "auto_enable_key"

// dailyRunGuard 每渠道每日执行守卫：mu 串行化并发 tick，date 记录当日执行日期（2006-01-02）。
type dailyRunGuard struct {
	mu   sync.Mutex
	date string
}

// autoEnableDailyRun daily 策略每渠道每日执行守卫：channelID -> *dailyRunGuard。
// 内存 map，重启后重新计一天，可接受。
var autoEnableDailyRun sync.Map

// AutoEnableKeyTask 扫描所有启用自动启用的渠道，按策略重新启用满足条件的 Key。
func AutoEnableKeyTask() {
	channels, err := op.ChannelList(context.Background())
	if err != nil {
		log.Errorf("auto enable key task failed to list channels: %v", err)
		return
	}
	now := time.Now()
	for i := range channels {
		channel := &channels[i]
		if !channel.AutoEnableKey || !channel.Enabled {
			continue
		}
		switch model.NormalizeAutoEnableStrategy(channel.AutoEnableStrategy) {
		case model.AutoEnableStrategyDaily:
			autoEnableDaily(channel, now)
		case model.AutoEnableStrategyBackoff:
			autoEnableBackoff(channel, now)
		default:
			autoEnableFixed(channel, now)
		}
	}
}

// autoEnableFixed 固定延迟：禁用满 AutoEnableDelayMin 分钟后启用。
func autoEnableFixed(channel *model.Channel, now time.Time) {
	delaySec := int64(channel.AutoEnableDelayMin) * 60
	for i := range channel.Keys {
		key := &channel.Keys[i]
		if !isCandidate(key) {
			continue
		}
		if now.Unix()-key.DisabledAt >= delaySec {
			enableKeyFunc(channel, key, now, delaySec)
		}
	}
}

// autoEnableBackoff 指数退避：延迟 = min(AutoEnableDelayMin × 2^(AutoBanCount-1), AutoEnableBackoffMaxMin) 分钟。
func autoEnableBackoff(channel *model.Channel, now time.Time) {
	for i := range channel.Keys {
		key := &channel.Keys[i]
		if !isCandidate(key) {
			continue
		}
		delaySec := int64(backoffDelayMin(channel, key)) * 60
		if now.Unix()-key.DisabledAt >= delaySec {
			enableKeyFunc(channel, key, now, delaySec)
		}
	}
}

// autoEnableDaily 每日定点：跨过配置 HH:MM 且当日未执行时，启用该渠道全部自动禁用 Key（无最短时长门槛）。
func autoEnableDaily(channel *model.Channel, now time.Time) {
	hh, mm, ok := parseDailyTime(channel.AutoEnableDailyTime)
	if !ok {
		log.Warnf("channel %d invalid auto_enable_daily_time %q, fallback to fixed strategy", channel.ID, channel.AutoEnableDailyTime)
		autoEnableFixed(channel, now)
		return
	}
	// 今日定点时刻未到，等下一个 tick
	if now.Hour()*60+now.Minute() < hh*60+mm {
		return
	}
	today := now.Format("2006-01-02")
	guard, _ := autoEnableDailyRun.LoadOrStore(channel.ID, &dailyRunGuard{})
	g := guard.(*dailyRunGuard)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.date == today {
		return // 当日已执行
	}
	enabled := 0
	for i := range channel.Keys {
		key := &channel.Keys[i]
		if !isCandidate(key) {
			continue
		}
		enableKeyFunc(channel, key, now, -1) // daily 无最短时长门槛，时长检查交给 op 侧跳过
		enabled++
	}
	g.date = today
	if enabled > 0 {
		log.Infof("daily auto enable: channel %d enabled %d keys", channel.ID, enabled)
	}
}

// backoffDelayMin 计算指数退避延迟（分钟），封顶 AutoEnableBackoffMaxMin。
func backoffDelayMin(channel *model.Channel, key *model.ChannelKey) int {
	base := channel.AutoEnableDelayMin
	if base <= 0 {
		base = 1
	}
	shift := key.AutoBanCount - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 20 { // 防止溢出
		shift = 20
	}
	delay := base << shift
	if max := channel.AutoEnableBackoffMaxMin; max > 0 && delay > max {
		delay = max
	}
	return delay
}

// isCandidate 自动启用候选：已禁用且曾经被自动禁用过（手动禁用因 AutoBanCount 已清零而被天然排除）。
func isCandidate(key *model.ChannelKey) bool {
	return !key.Enabled && key.AutoBanCount > 0
}

// enableKeyFunc 是启用 Key 的间接调用点（测试中可替换为探针以验证策略选择）。
// now/delaySec 由策略层传入：op 侧在锁内以最新状态复核「仍禁用 + 已到期」。
var enableKeyFunc = enableKey

// enableKey 启用单个 Key：保留 AutoBanCount（退避语义），重置熔断器，
// 走 op.ChannelKeyEnableIfDue 原子检查并置位（内存缓存 + 脏标记路径）。
// 不再就地修改传入的 key（避免对共享 Keys 数组的并发写），实际启用以 op 锁内最新状态为准。
func enableKey(channel *model.Channel, key *model.ChannelKey, now time.Time, delaySec int64) {
	balancer.ResetCircuit(channel.ID, key.ID)
	enabled, err := op.ChannelKeyEnableIfDue(channel.ID, key.ID, now, delaySec)
	if err != nil {
		log.Warnf("auto enable key %d failed: %v", key.ID, err)
		return
	}
	if enabled {
		log.Infof("auto enabled channel key %d (channel %d, strategy %s)", key.ID, channel.ID, channel.AutoEnableStrategy)
	}
}

// parseDailyTime 解析 "HH:MM"（00:00–23:59），返回 (hour, minute, ok)。
// "24:00"（凌晨24点）按 00:00 处理，兼容直接写库/API 传入的情况。
func parseDailyTime(s string) (int, int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, false
	}
	// 必须为 ASCII 数字，否则非数字字符经 byte 运算可能凑出合法数值（如 "0::00" -> 10:00）。
	for _, i := range [4]int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return 0, 0, false
		}
	}
	hh := int(s[0]-'0')*10 + int(s[1]-'0')
	mm := int(s[3]-'0')*10 + int(s[4]-'0')
	if s == "24:00" {
		hh, mm = 0, 0
	}
	if hh > 23 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}
