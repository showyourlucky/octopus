# internal/helper 工作记忆

## 渠道 URL 延迟探测

- `ChannelBaseUrlDelayUpdate` 是渠道 Base URL 延迟探测的公共入口，定时任务和渠道创建/更新后的异步刷新都会调用它。
- 未启用渠道（`channel.Enabled == false`）必须在该入口直接跳过，不发起 `HEAD` 探测请求，避免禁用渠道的无效域名或不可达上游持续产生日志噪声。
- 跳过未启用渠道只记录调试日志，不应按失败探测输出 `Warn`。
