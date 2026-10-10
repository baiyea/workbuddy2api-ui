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


## Task 3: 接入 Responses 创建接口及明文历史

**Files:** `extensions/internal/responses/adapter.go`、`adapter_test.go`、`extensions/cmd/server/extension.go` 及 wiring 测试；`patches/0009-relaykit-responses-history.patch`（含库回归测试）、`patches/series`、`patches/README.md`。

**Interfaces:** `responses.New(next http.Handler, apiKey string, maxBodyBytes int64) http.Handler`。仅截获 `/v1/responses`；认证为单个 Authorization Bearer。其他请求原样交给 next，无 bridge 不包装。

- [ ] **先写路由失败测试。** POST 文本/工具/图片/schema 经 New 到达 `/v1/chat/completions`；GET 为 405，重复/错误认证为 401，models/chat 路径不变。
- [ ] **实现入口并接线。** 使用 Serve/WriteError；保持 Handler 和 bridge context。沿用明确的 query、压缩体限制。
- [ ] **先写工具状态失败测试。** function_call_output 保留 call_id；custom 工具依靠请求元数据在响应中恢复；两个并发请求的名称/参数不串。
- [ ] **先写 reasoning 回传失败测试。** 将生成的 reasoning.summary、assistant 文本和 function_call 作为下轮 input；断言 reasoning_content 和工具关联均保留。encrypted/opaque reasoning 必须在调用 next 前 400。
- [ ] **制作最小历史补丁。** 只补 Responses 明文 reasoning 请求映射；原始快照保持不变，补丁附测试及移除条件。重物化确认原先 RED 的测试 GREEN。
- [ ] **验证完整响应与流。** 文本/推理/工具、length 状态、连续序号及 final output 聚合一致；store/background true 和 previous_response_id 等为 400，false 可用。
- [ ] **验证。** 新物化 `.build/protocol-check-3`，运行库 `test ./...` 以及 `go -C .build/protocol-check-3 test ./internal/responses ./internal/protocol ./internal/server ./cmd/server`；预期退出 0。


Read root AGENTS.md and the referenced design spec. Do not create worktrees or branches. Do not spawn subagents. Keep reports/logs under docs/superpowers.
