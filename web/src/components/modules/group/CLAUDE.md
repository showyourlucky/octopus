# 分组模块工作记忆

## 分组卡片

- `Card.tsx` 中的分组卡片默认隐藏已选模型列表，只显示模型数量摘要；点击摘要栏后再展开并渲染 `MemberList`，避免分组页初始加载时一次性渲染大量模型造成滚动卡顿。
- 展开时先渲染轻量 loading + 骨架行占位，等待连续两帧后再按 `getMembersLoadingDelay(count)` 的模型数量分层延迟挂载 `MemberList`；少量模型不额外等待，大量模型保留可感知反馈，避免固定延迟让小分组显得拖沓。
- 展开后的 `MemberList` 仍负责模型排序、删除、权重编辑和拖拽重排；不要把这些交互拆散到卡片外层，避免破坏既有提交逻辑。
- `Card.tsx` 展开分组模型列表时给 `MemberList` 传入 `virtualizeLargeList`；超过 80 个模型时切换为 `@tanstack/react-virtual` 虚拟滚动，只挂载可见行，保留删除、权重编辑、置顶和置底，避免完整挂载 `@hello-pangea/dnd` 导致展开后滚动卡顿。
- 虚拟滚动模式会关闭卡片预览中的拖拽排序，必须显示性能模式提示；完整拖拽排序保留在编辑弹窗中。
- 虚拟滚动逻辑放在独立子组件中，只在大列表性能模式挂载 `useVirtualizer`，避免小列表也初始化虚拟器。
- 成员删除确认态由 `MemberList` 父级按 `member.id` 维护，避免虚拟行滚出视口卸载后丢失确认状态。
- `MemberList` 的成员行使用 `React.memo`；`content-visibility: auto` 只用于无 DnD 的虚拟滚动预览行，不要加到 `@hello-pangea/dnd` 拖拽行，否则拖拽库测量与定位可能偏移到视线外。
- `MemberList` 小列表仍使用 `@hello-pangea/dnd` 拖拽排序；完整拖拽路径必须保持原生布局测量，不要添加 CSS contain。
- `@hello-pangea/dnd` 拖拽中的行通过 `createPortal` 挂载到 `document.body`，避免上层 `motion` transform、弹窗或滚动容器改变 fixed 坐标系后把拖拽项偏移到视线外。
- 卡片折叠状态只影响卡片展示，不影响编辑弹窗中的模型选择、排序和保存流程。
- 卡片头部操作按钮不要使用 `layoutId` 参与共享布局动画；编辑按钮使用 `useMorphingDialog` 接入 `triggerRef`、aria 和键盘事件，保证关闭弹窗后可恢复焦点，同时避免头部按钮共享布局动画导致展开时不同步下移。
