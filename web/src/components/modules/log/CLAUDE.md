# 日志模块工作记忆

## 日志列表滚动

- 主应用容器使用 `h-dvh` 和 `overflow-hidden` 锁定整屏布局，日志列表内容变长时必须由日志页内部容器负责纵向滚动。
- `index.tsx` 的 `PageWrapper` 需要保留 `h-full min-h-0 overflow-y-auto overscroll-contain`，否则日志卡片超过视口后无法向下滑动，也会影响底部加载更多哨兵进入视口。
- 底部 `loadMoreRef` 依赖滚动容器触达底部后被 `IntersectionObserver` 观察到；调整布局时不要移除底部占位和 `pb-24 md:pb-4` 的移动端安全距离。
