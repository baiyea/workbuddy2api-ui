# Global Constraints

- 不修改 `upstream/` 或 `upstream-convert/`；既有快照文件和第三方库修正通过 `patches/`。
- RelayKit 固定本地 New API commit `0f38a14a5e8e322ea741478b0e49878be3130e1d`；保留原 module 路径、许可证与署名。
- 保持两个服务、同一账号池、无 HTTP 回环；只在 bridge 分支包装。
- 未知用量为 null、真实零保留、采用最后明确累计值；不伪造成功终态。
- 请求限制使用现有配置；JSON 响应和 SSE 累计上限均为 32 MiB，SSE 单帧 8 MiB。
- 保留认证、owner 上下文、取消；不调用真实上游、不重启实际服务。
- 不新增 Files API、response 存储、后台工具执行、JSON Schema 修复或管理业务。
- 所有文档/中间记录位于 `docs/superpowers`；保留原有 `.gitignore`、探测报告和 `upstream-convert/`。
- 不自动发布、推送、部署；确认实施只授权本地开发和隔离验证。


## Task 4: 增强 Anthropic 并修正混合历史

**Files:** `extensions/internal/anthropic/adapter.go`、`stream.go`、现有测试和新增 `conversion_test.go`；`extensions/internal/protocol/claude_validation.go` 及测试；`patches/0010-relaykit-claude-history.patch`（含库回归）、`patches/series`、`patches/README.md`。

**Interfaces:** 保持 anthropic.New、WithConversation、bridge envelope。`validateClaudeRequest(raw []byte, input *dto.ClaudeRequest) error` 负责嵌套形状验证，不承担协议转换。

- [ ] **先补入口失败测试。** `TestToolDefinitionsCallsAndResults`、`TestImagesAndGenerationParameters`、`TestThinkingOutput`、`TestToolStreamKeepsLateZeroUsage` 在旧文本实现上确认失败，记录失败原因。
- [ ] **先写库缺口失败测试。** 完整 assistant text+tool_use 历史和随后 tool_result，断言 Chat 同时保留文本、tool_calls、关联 ID；无签名明文 thinking 保留 reasoning_content。原始库应失败。
- [ ] **制作最小库补丁。** 只修该方向已证实的内容丢失，保持公开 API/模块路径；回归与改动一并写入 0010。外部非空 signature/redacted 块由宿主返回带路径 400，不伪造或悄悄删除。
- [ ] **替换文本转换与旧传输。** 保留 x-api-key/version/beta/query/compression 检查；读取可信 context 会话后调用 Serve。仅删除共享层已替代的代码。
- [ ] **完善验证及旧测试。** 保留 malformed/null/角色/缺字段/未知字段/非法图片校验；只移除因能力扩展过时的“工具/图片/参数必拒绝”断言。认证、取消、owner、未知/晚到用量及错误回归保留。
- [ ] **验证。** 新物化 `.build/protocol-check-4`，运行库 `test ./...`，以及 core `test ./internal/anthropic ./internal/protocol ./internal/bridge ./cmd/server` 和相关包 race；新增及现有契约均须通过。


Read root AGENTS.md and the referenced design spec. Do not create worktrees or branches. Do not spawn subagents. Keep reports/logs under docs/superpowers.
