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
