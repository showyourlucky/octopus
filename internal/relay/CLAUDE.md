# internal\relay 目录说明

- `relay.go` 负责主文本请求的转发、流式响应透传、重试与空响应判定。
- 处理流式 SSE 时，`empty stream response` 仍以“是否真正向客户端产出有效流内容”为准，不要把 `choices=[]`、仅 `usage` 或其他纯元数据 chunk 视为有效回复。
- 遇到中间态空 chunk 时，不应把它误当成有效内容；若整个流最终都没有产出有效内容，仍应返回空响应失败。
- 为了让空流仍可进入现有重试链路，首个有效内容出现前的 SSE 元数据事件需要先缓冲，确认非空后再一次性向客户端回放。
- 协议差异统一依赖 outbound adapter 先把 OpenAI Responses、Anthropic Messages、Gemini 等流事件映射到 `InternalLLMResponse`，空回复测试也应优先覆盖“原始事件 -> 统一结构 -> 判空”这条链路。
- 修改该目录代码时，优先保持现有重试、首 token 超时和适配器调用顺序不变，避免破坏渠道切换行为。

## 渠道 Key 负载方式

- `key_load_balance_mode` 默认值为 `failover`，空值或未知值也应按故障转移处理。
- `failover` 按 Key ID 稳定排序，优先使用第一个可用 Key；只有该 Key 失败且启用多 Key 重试时，才继续尝试后续 Key。
- `round_robin` 仍按 `LastUseTimeStamp` 升序选择最久未使用的 Key；`random` 仍在请求前随机打乱候选 Key。

## 分组外模型精确匹配

- API Key 为 `all_channel` 且请求模型不是分组名时，relay 会按模型名精确匹配启用渠道，并构造运行时临时分组继续复用现有迭代器、重试、熔断和日志链路。
- 临时分组使用故障转移语义；某渠道请求该模型失败后，`balancer` 会记录近期失败并在 `circuit_breaker_cooldown` 冷却期内把该渠道排到后面。
- 近期失败只影响分组外模型的候选排序，不直接禁用渠道；真实分组仍完全遵循分组自身的模式、优先级、权重和会话保持设置。

## 渠道模型测试

- 管理端渠道模型测试通过 `relay.ProbeChannelModel` 固定单个渠道发起真实非流式 Chat 请求，必须复用现有出站适配、Key 选择、熔断、统计、日志与自动禁用 Key 逻辑。
- 测试请求的日志 `request_api_key_name` 固定为 `渠道测试`，并且不能调用 `StatsAPIKeyUpdate`，避免计入设置页平台 API Key 统计。
- 测试请求会计入总统计、渠道统计、日志、熔断和自动禁用 Key；这是设计行为，不要改成无副作用探测。
- 管理端测试响应只返回测试摘要，不能把上游原始响应直接写给前端接口；relay 中使用 `suppressResponse` 控制这一点。
- `ProbeChannelModel(c, channel, model, keyID *int)` 第四个参数用于锁定单 Key 精确测试。非 nil 时通过 `relayRequest.forceKeyID` 传到 `executeRelay`，由 relay 在 `GetCandidateKeys` 之后过滤候选并强制 `maxAttempts = 1`，绕过渠道 `EnableMultiKeyRetry` 的跨 Key 重试。**不要尝试在 handler 层通过修改 `channel.Keys` 实现锁定**——`executeRelay` 会用 `op.ChannelGet(item.ChannelID, ...)` 从缓存重新加载完整 channel，handler 的修改会被静默覆盖（这是一个已踩过的坑）。

## 渠道 Key 自动启用

- relay.go 自动禁用点：触发禁用时 `AutoBanCount++`、写入 `DisabledAt=now`，remark 格式保持 ` [Auto banned: %d failures]` 不变。
- `internal/task/auto_enable_key.go` 扫描器每 5 分钟（启动即跑一次）遍历内存渠道缓存，对满足 `!Enabled && AutoBanCount>0` 的 Key 按渠道策略（fixed / daily / backoff）自动启用：置 `Enabled=true`（**AutoBanCount 保留不归零**，保留退避语义）、重置熔断器 `balancer.ResetCircuit(channelID, keyID)`（清理该 key 全部 model 维度条目，否则 `Iterator.SkipCircuitBreak` 会继续跳过它）、走 `op.ChannelKeyEnableIfDue`（内存缓存 + 脏标记路径，落库仍由优雅停机 `SaveCache` 触发）。
- 手动改 Key 状态（`KeysToUpdate` 中 `Enabled` 非 nil）会把 `AutoBanCount` 清零；渠道开关 false→true 会把该渠道所有自动禁用 Key 的 `DisabledAt` 重置为当前时刻（计时重新开始，不补启用）。
- 并发正确性：`op.ChannelKeyUpdate` / `op.ChannelKeyEnableIfDue` 通过 `channelCacheMu` 串行化 `channelCache` 的读-改-写，避免并发更新互相覆盖（lost update）——扫描器自动启用与 relay 对同渠道其它 Key 的更新并发时，启用结果不会被旧快照覆盖。`enableKey` 不再就地修改传入的 key；实际启用由 `ChannelKeyEnableIfDue` 在锁内以最新状态复核「仍禁用 + 已到期」后原子完成。
- 渠道编辑/开关（`ChannelUpdate` / `ChannelEnabled`）在事务前先 `ChannelKeySaveDB` 落库运行时 Key 状态，避免随后的缓存重载把内存态（自动禁用/自动启用）回滚。
- 存量/手动禁用的 Key 因 `AutoBanCount=0` 永远不会被扫描器自动启用。
