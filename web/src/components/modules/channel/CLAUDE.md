# 渠道模块工作记忆

## 渠道 Key 识别

- 渠道表单的每个 Key 行必须显示只读 ID 标签：已保存 Key 显示 `ID: xxx`，新建未保存 Key 显示“保存后生成 ID”。
- 渠道详情查看态的 Key 列表也必须显示 `ID: xxx`，用户不进入编辑也能把日志中的 `key_id=xxx` 对应到具体 Key 行。
- 日志中的 `key_id=xxx` 需要能回到渠道详情或编辑弹窗中快速定位到对应 Key 行，因此不要隐藏或移除 `CardContent.tsx`、`Form.tsx` 中的 Key ID 标签。
- 不要用真实 Key 内容或后四位作为主要识别方式，避免泄露敏感信息；备注用于人类可读名称，Key ID 用于精确定位。
- 编辑弹框使用 `md:max-w-3xl`，Key 行桌面端使用 `md:flex-nowrap`，避免加入 Key ID 标签后删除按钮被挤到下一行；移动端仍允许换行保证可用。

## Key 负载方式说明

- 新建渠道默认 `key_load_balance_mode` 为 `failover`，不要再把前端默认值改回 `round_robin`。
- 负载方式下拉框必须包含 `failover`、`round_robin`、`random`，并在下方展示当前选中模式的详细说明。
- 说明文案可能较长，表单中使用固定最大高度和 `overflow-y-auto` 展示，避免编辑弹窗被长说明撑高。

## 渠道模型测试入口

- 每张渠道卡片内的 Flask 图标按钮打开 `ModelTestDialog`，点击按钮必须 `stopPropagation`，避免误触发卡片详情弹层。
- 测试弹层只展示当前渠道 `model` 与 `custom_model` 合并去重后的模型，并且第一版只允许 Chat 类渠道测试。
- 该测试会触发真实上游请求，结果会进入现有日志、统计、熔断和自动禁用 Key 逻辑；前端状态文案需要明确这是“真实模型请求”。
- 弹层包含 Key 选择器：默认选中第一个 `enabled && channel_key` 不为空的 Key，显示文本沿用 `ID: xxx (备注)` / `ID: xxx` 约定，便于和日志 `key_id=xxx` 对应；选中值通过 `key_id` 透传后端，由 handler 把 `channel.Keys` 收敛为该 Key 后再走 relay。
- 弹层另含「允许其他 Key 接力」Switch：默认 OFF 走精确单 Key 诊断；ON 时禁用 Key 选择器并**不传** `key_id`，由后端走渠道默认负载策略 + `enable_multi_key_retry` 自然工作。开关 ON 时如果渠道本身没开多 Key 重试，效果只是起点 Key 由 balancer 决定，不会触发跨 Key 重试——文案需明确这点。
- 不要尝试在 handler 层重排 `channel.Keys` 来"指定 Key 优先 + 失败回退"：`Channel.GetCandidateKeys()` 会按 `key_load_balance_mode` 重排（failover 按 ID 升序、round_robin 按 LastUseTimeStamp、random 随机），handler 的重排会被覆盖。更关键的是 `executeRelay` 会通过 `op.ChannelGet` 从缓存重新加载完整 channel，handler 层任何对 `channel.Keys` 的收敛都会被静默覆盖。Key 锁定必须由后端 `relayRequest.forceKeyID` 在 relay 层执行——见 `internal/relay/CLAUDE.md` 渠道模型测试段。前端只需把 `key_id` 透传给 `/api/v1/channel/test-model`。
- 弹层使用两个独立的 `useEffect`：**Effect 1 只依赖 `open` 状态**负责打开时 `resetTestState()`、关闭时清空 `selectedKeyId` 和 `allowKeyFallback`；**Effect 2 依赖 `enabledKeys`** 但只在"当前选中已失效"时回到首个 Key。不要把这两件事合到一个依赖 `enabledKeys` 的 effect 里——`useTestChannelModel.onSettled` 会 `invalidate ['channels','list']`，导致 `channel.keys` 引用变化，合并写法会把刚出来的成功/失败结果立刻擦掉。
- `StatusBox` 内文本必须使用 `min-w-0 break-all wrap-anywhere` + 外层 `max-w-full overflow-hidden`，否则错误信息中的长 URL 或无空格字符串会突破 Dialog 边界。Tailwind v4 中正确 utility 是 `wrap-anywhere`（不是 `overflow-wrap-anywhere`）。
