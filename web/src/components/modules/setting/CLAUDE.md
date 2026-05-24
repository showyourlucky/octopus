# 设置模块工作记忆

## API Key 模型白名单

- API Key 表单的 `all_channel` 模式只展示分组模型和启用渠道模型；禁用渠道模型不能作为白名单候选。
- 从 `all_channel` 切回 `group` 时继续清理渠道专属模型，避免提交界面不可见的隐藏白名单。
- 前端候选列表必须与后端 `APIKeyVisibleModels` 和 relay 实际可请求范围保持一致。
