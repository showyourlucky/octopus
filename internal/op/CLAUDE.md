# internal/op 工作记忆

## API Key 全渠道模型

- `APIKeyVisibleModels` 在 `all_channel` 模式下只能合并启用渠道的模型；`ChannelLLMList` 会返回 `Enabled` 字段，调用方需要按实际可请求能力过滤。
- 禁用渠道独有模型不能出现在 `/v1/models` 或 API Key 白名单候选中，否则会出现列表可见但 relay 精确匹配返回 `model not found` 的不一致。
- 修改全渠道模型逻辑时要同步覆盖 `APIKeyVisibleModels` 的回归测试，避免破坏 `group` 模式和 `supported_models` 白名单行为。
