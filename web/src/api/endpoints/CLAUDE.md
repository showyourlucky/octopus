# API endpoints 工作记忆

## 分组缓存刷新

- `useUpdateGroup` 更新成功后需要先用接口返回的完整 `Group` 同步写入 `['groups', 'list']` 缓存，再触发 `invalidateQueries` 后台刷新；否则编辑弹窗关闭后，分组卡片会先回到旧缓存，用户看到保存结果没有刷新。
- 只在返回数据包含有效 `id` 时替换列表中的同 ID 分组，避免异常响应污染缓存。

## API Key 类型字段

- `APIKey.model_access_type` 可选值为 `group`、`all_channel`；前端空值按 `group` 处理，保持旧数据兼容。
- API Key 表单在 `group` 模式下只展示分组模型；切到 `all_channel` 时展示分组模型与渠道模型去重集合。
- 从 `all_channel` 切回 `group` 时，需要清理白名单中渠道专属模型，避免提交隐藏的不可选模型。

## 渠道模型测试接口

- `useTestChannelModel` 调用 `POST /api/v1/channel/test-model`，参数为 `channel_id`、`model`、可选 `key_id`。
- `key_id` 未传时由后端按渠道默认负载策略选择；传入时后端会过滤 `channel.Keys` 仅留指定 Key 进行测试，匹配不到或被禁用会返回 400 `指定的 Key 不存在或已禁用`。
- 该接口会触发真实上游 Chat 请求，并会产生日志、总统计、渠道统计、熔断和自动禁用 Key 副作用；前端文案不要暗示这是无副作用的本地校验。
- 测试完成后需要刷新 `['channels', 'list']`，让渠道卡片统计及时体现测试请求结果。
