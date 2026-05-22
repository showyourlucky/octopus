# internal/task 工作记忆

## 自动同步模型

- `SyncModelsTask` 在调用上游获取模型前必须同时判断：
  - `channel.AutoSync == true`
  - `channel.Enabled == true`
- 未启动/禁用渠道只记录调试日志并跳过，不参与上游模型获取、渠道模型更新、自动分组和本轮价格表同步统计。
