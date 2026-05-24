# 日志模块工作记忆

## 日志列表滚动

- 主应用容器使用 `h-dvh` 和 `overflow-hidden` 锁定整屏布局，日志列表内容变长时必须由日志页内部容器负责纵向滚动。
- `index.tsx` 的 `PageWrapper` 需要保留 `h-full min-h-0 overflow-y-auto overscroll-contain`，否则日志卡片超过视口后无法向下滑动，也会影响底部加载更多哨兵进入视口。
- 底部 `loadMoreRef` 依赖滚动容器触达底部后被 `IntersectionObserver` 观察到；调整布局时不要移除底部占位和 `pb-24 md:pb-4` 的移动端安全距离。

## 渠道 Key 诊断展示

- 后端 `attempts` 中已有 `channel_key_id` 和 `channel_key_remark`，前端展示尝试记录时必须至少显示 `key_id=xxx`，不能只显示渠道名，否则同一渠道配置多个 Key 时无法定位限流或上游错误来源。
- Key 备注存在时显示为 `备注 / key_id=xxx`；没有备注时仍显示 `key_id=xxx`。
- 单次尝试日志也要在卡片渠道 Badge 和详情诊断区显示 Key 信息，不要只在多次重试 Tooltip 里展示。
- 日志详情诊断区的渲染条件必须包含 `hasAttemptDetails`；单次成功请求通常没有错误也没有重试，但仍需要展开入口查看尝试明细和 Key 信息。
