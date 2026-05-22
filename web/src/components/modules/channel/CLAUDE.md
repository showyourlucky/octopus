# 渠道模块工作记忆

## 渠道 Key 识别

- 渠道表单的每个 Key 行必须显示只读 ID 标签：已保存 Key 显示 `ID: xxx`，新建未保存 Key 显示“保存后生成 ID”。
- 渠道详情查看态的 Key 列表也必须显示 `ID: xxx`，用户不进入编辑也能把日志中的 `key_id=xxx` 对应到具体 Key 行。
- 日志中的 `key_id=xxx` 需要能回到渠道详情或编辑弹窗中快速定位到对应 Key 行，因此不要隐藏或移除 `CardContent.tsx`、`Form.tsx` 中的 Key ID 标签。
- 不要用真实 Key 内容或后四位作为主要识别方式，避免泄露敏感信息；备注用于人类可读名称，Key ID 用于精确定位。
- 编辑弹框使用 `md:max-w-3xl`，Key 行桌面端使用 `md:flex-nowrap`，避免加入 Key ID 标签后删除按钮被挤到下一行；移动端仍允许换行保证可用。
