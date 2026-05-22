# OpenAI 出站转换记忆

## 小米 MiMo reasoning_content 回传

- 小米 MiMo 思考模式在多轮 Agent 会话中，如果历史 `assistant` 消息包含 `tool_calls`，后续请求必须完整回传该消息的 `reasoning_content`，否则上游会返回 400。
- `ChatOutbound.TransformRequest` 仍默认调用 `request.ClearHelpFields()` 清理内部辅助字段；仅当 `baseUrl` 包含 `xiaomimimo.com` 或模型名以 `mimo-` 开头时，才会在清理前暂存并恢复带工具调用的历史 `assistant.reasoning_content`。
- 该逻辑只针对 OpenAI Chat Completions 出站请求，避免把内部思考字段扩散到普通 OpenAI 兼容上游。
