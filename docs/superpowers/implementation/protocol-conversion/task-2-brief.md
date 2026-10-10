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


## Task 2: 建立共用 RelayKit 传输层

**Files:** 新增 `extensions/internal/protocol/adapter.go`、`stream.go`、`usage.go`、`responses_validation.go` 及对应 `_test.go`。Task 4 添加 `claude_validation.go`；本任务只保留 Claude 文本的必要验证，不开放增强入口。

**Interfaces:**

```go
func Serve(w http.ResponseWriter, r *http.Request, next http.Handler,
    apiKey string, maxBodyBytes int64, format types.RelayFormat, conversationID string)
func WriteError(w http.ResponseWriter, code int, message string, format types.RelayFormat)
```

format 仅为 RelayFormatOpenAIResponses 或 RelayFormatClaude。Serve 负责有界解析、验证、转换及 next 调用；调用方负责路由/方法/认证/协议头。conversationID 仅由可信 context 提取。错误包不能透出原始上游诊断内容。

- [ ] **先写请求边界失败测试。** `TestRequestIsolationAndConvertedLimit` 断言 context 取消/owner 保留、公共私有头剥离、转换体超限不调用 next。`TestInvalidRequestsDoNotReachHandler` 覆盖 null、尾随 JSON、空模型、非法布尔值/嵌套结构及私有字段，预期 400。
- [ ] **实现请求转换。** 公开 ConvertRequest，每请求新建 convmeta.Values 并跨两端复用；safe 策略后检查完整 diagnostics。处理无状态参数，输出上限映射 max_tokens，重建请求后进程内调用 next。
- [ ] **先写响应/用量失败测试。** `TestUnknownUsageRemainsNull`、`TestLateZeroUsageWins`、`TestPartialUsagePreservesObservedFields`、`TestNonStreamingErrorIsSanitized` 分别断言 null、输入 0/输出 2、最后明确值及脱敏。total 最后明确值优先（包括 0），从未提供才在两项均已知时求和。
- [ ] **实现普通响应。** 至多 32 MiB 捕获 next；验证 choice、finish_reason、usage 后调用 ConvertResponse，序列化时用原始 presence 修正用量。
- [ ] **先写流式失败测试。** `TestFirstDeltaIsImmediate` 在 next 未返回时检查文本已输出；`TestTerminalWaitsForLateUsage` 在 finish 后检查尚无终态；`TestTruncatedStreamCannotComplete` 覆盖 DONE 无 finish、残帧、错误包、finish 后 choice 和 DONE 后 data。
- [ ] **补状态、取消、容量测试。** `TestIncompleteFinishReasons` 断言 length/content_filter 的 Responses 为 incomplete；未知 reason 失败。`TestWriteAndFlushFailureCancelNext`、`TestStreamTotalLimit` 覆盖短写/写入/Flush 失败、8 MiB 单帧和 32 MiB 累计上限，断言取消传到 next 且无成功终态。
- [ ] **实现 SSE 转换。** 有界逐帧解析并记录 finish/usage，内容去掉两者后喂库；Claude 的 reasoning/text/tools 混合 delta 拆分。正常 EOF 且 finish 合法后合成终止 chunk 再 Finalize，覆盖 nullable usage；Responses 开启 sequence_number。
- [ ] **验证。** 每批测试先 RED 再 GREEN；新物化 `.build/protocol-check-2`，执行该目录 `go test ./internal/protocol`、`go test -race ./internal/protocol`、`go vet ./internal/protocol`。环境不能运行 race 时记缺口，不写通过。


Read root AGENTS.md and the referenced design spec. Do not create worktrees or branches. Do not spawn subagents. Keep reports/logs under docs/superpowers.
