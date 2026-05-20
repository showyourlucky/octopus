# API endpoints 工作记忆

## 分组缓存刷新

- `useUpdateGroup` 更新成功后需要先用接口返回的完整 `Group` 同步写入 `['groups', 'list']` 缓存，再触发 `invalidateQueries` 后台刷新；否则编辑弹窗关闭后，分组卡片会先回到旧缓存，用户看到保存结果没有刷新。
- 只在返回数据包含有效 `id` 时替换列表中的同 ID 分组，避免异常响应污染缓存。
