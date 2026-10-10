# Execution ledger — plan: docs/superpowers/plans/2026-10-10-protocol-conversion.md

- Baseline: main，原有改动 .gitignore；未跟踪 upstream-convert/ 与 model-capability-probe-20261010.json。
- 已创建 codex/protocol-conversion 分支；使用现有目录以复用用户未跟踪的第三方源码，保留原有修改。
- 用户最新指令覆盖此前开发授权：先写设计，再写计划，最后取得实施确认。当前状态：等待用户审阅两份文档与确认，不能恢复开发。
- 此前初稿不计为完成。所有本轮源码/构建草稿已移动或备份到 deferred-protocol-20261010/，四个原本干净的 tracked 构建文件已恢复。git diff 仅余用户原有 .gitignore 改动；未触碰 upstream-convert/。
- 两个下载/依赖进程已由其创建者停止；已有便携 Go 与缓存保留，未重启实际服务。
- Pre-flight: Task 1 提供模块；Task 2 提供 protocol.Serve / WriteError；Task 3/4 接入协议并分别维护历史补丁；Task 5 集成验收。
- 环境：Go 不在 PATH，但发现既有 .build/dev-tools/go/bin/go.exe；Docker Desktop Linux 引擎未运行。当前只做文档，不再下载或构建。
- 静态独立复核发现 Responses reasoning.summary 回填丢失；已纳入设计及 Task 3 最小库补丁与回归。用量 presence/total、终止原因、无签名历史回传也已明确。
- 初步 Anthropic 增强测试仅观察到预期 RED，构建打包单点测试曾 RED→GREEN；这些草稿现已归档，不代表本功能通过。未做功能 GREEN、完整 SDK、容器或真实上游验收。

## 已确认实施（新阶段）

- 用户授权按计划实施直至集成验证成功，不创建 worktree 或新分支。
- 当前分支 codex/protocol-conversion；基线 8cbf7ae9943e85bda92f81778b056435479fa954。
- Docker 引擎现已可用（29.1.3），需要执行真实隔离容器验收。

| 检查 | 接口/约束 | 结论 |
| --- | --- | --- |
| Task 1 → 2 | 本地 RelayKit 模块及 Go 版本 | 一致；模块依赖补丁不依赖运行服务 |
| Task 2 → 3/4 | Serve/WriteError、nullable usage、请求 meta | 一致；Task 4 扩展 Claude 验证，不重复传输 |
| Task 3 ↔ 4 | extension 接线与 patches/series | 串行执行避免覆盖；分别 0009/0010 |
| Task 1 → 5 | build/check 脚本和 SDK 验证 | 新增协议 race 列表在包存在后添加 |
| Task 1 | 来源/打包测试 | 无冲突 |
| Task 2 | 流/总量/结束检查 | 无冲突 |
| Task 3 | Responses 历史补丁与入口 | 无冲突 |
| Task 4 | Anthropic 历史补丁与信任边界 | 无冲突 |
| Task 5 | SDK、容器、原路径回归 | 无冲突；全部必须实际通过 |

- Task 1: complete (commit 884cbdc5, review protocol_review1: spec compliant/quality approved, no findings). Desktop11, check.sh full native incl race, Go1.26.9 Linux core+RelayKit test/vet,162 source hashes verified. Windows scripts/deploy POSIX failures remain a Task5 Linux integration check, not waived.
- Task 2: starting from approved Task1; request metadata shared only within one request, no early completion or usage estimation.
- Task 2: implementer protocol_task2, base 728f5a0784af77b58b2cb134c3fbd8682fd5d791; user explicitly reconfirmed subagent implementation and review through final integration. No additional approval gate, branch or worktree.
- Ruling: Task 2 may update the existing 0008 module patch for transitive dependencies required by real converter imports — Task 1's registry-only import did not exercise the full dependency graph — verify final imports using a fresh overlay and -mod=readonly.
- Linux baseline evidence: git -c core.autocrlf=false archive of 728f5a0, isolated wb2a-protocol-checkenv:local, network disabled: scripts44/deploy13/desktop11 all passed. These close the platform-specific baseline failures only; final feature integration remains required. Log: implementation/protocol-conversion/linux-baseline-python.log.
- Baseline export diagnosis: ordinary Windows git archive applied core.autocrlf and emitted CRLF although committed shell blobs are LF. Correct export with process-local core.autocrlf=false passed; no production source change made for this diagnostic. Actual Windows working-copy Docker entrypoint remains CRLF and needs attention during packaging validation.
- Ruling: Task2 rejects Responses reasoning input until Task3 installs the library history fix; Task3 updates the shared validator alongside its patch to accept verified plaintext summary. This prevents silent content loss during development without narrowing the final history requirement.
- Task 2: implemented in 5b95153e91d87300d6b2d91b8b3647798fc00a6a; fresh-overlay protocol readonly test/race/vet and all22 core packages passed. Independent task review protocol_review2 is running against the full 728f5a0..5b95153 diff. Not marked complete until that gate passes.
- Task 2: review found one Important issue: malformed/unfinished tool outputs can be dropped by RelayKit and incorrectly complete. Fix round1 assigned to protocol_task2; report task-2-review.md. Cross-task unknowns are covered by Task1 provenance and Task3/4/5 integration requirements.
- Task 2: fix round1/5 (1 addressed, 0 open; commit7607e56, scoped review protocol_review2_fix1 clean). Ordinary and fragmented tool structure now validated before completion; both-format RED/GREEN and readonly test/race/vet passed.
- Task 2: complete (5b95153 + 7607e56; spec issue resolved, quality approved). Task3 must enable validated plaintext reasoning history with its library patch; Task4 must replace temporary Claude text-only request checks.
- Integration tooling: SDK Linux image built, actual Python3.12.15/Go1.26.9/Anthropic0.67.0/httpx0.28.1/OpenAI2.6.1 imports, Docker/Compose/buildx and baseline in-memory SDK preflight passed. This is preparation, not final feature validation.
- Task 2 production-toolchain check: archive8954bef, network-disabled Linux container Go1.26.9, fresh overlay, protocol readonly test/race/vet all passed (0.311s/4.684s/no vet output). Log task-2-linux-production-toolchain.log; actual new shared code was compiled/tested, not just the vendor library.
- Task 3: protocol_task3 implementing from base8954bef4a8c1ee464c8d99ca657dd8fa914fbef7; own public Responses wrapper, bridge wiring, reasoning history patch and shared plaintext validation.
- Task 3 ruling: allow only the exact inert fields emitted by the pinned ResponsesOutput serializer on reasoning history (content:null, role/quality/size empty strings), with a regression passing the generated object directly back as input. Nonempty/opaque/encrypted values remain rejected with field paths; no generalized unknown-field discard. This implements the existing output-roundtrip requirement.
- Task 3: implemented in 5602eb2b9a5ce53de28dc062c494f86ec41fb39e; independent reviewer protocol_review3 is checking full base8954bef..5602eb2. Core/race/library logs passed; empty vet log paths are absent, so final Task5 check.sh will provide fresh production-toolchain vet evidence.
- Task 3: review found one Important history-order issue: streamed function_call before reasoning split one assistant turn during input roundtrip. Fix round1 assigned to original protocol_task3 with actual generated-output and library regression; pending scoped review before completion.
- Task 3: fix round1/5 (1 addressed, 0 open; c96da82; protocol_review3_fix1 approved). Six actual SSE final-output ordering cases and library same-turn regressions RED/GREEN, focused core/race passed.
- Task 3: complete (5602eb2 + c96da82, spec issue resolved and quality approved). Cross-task checks: Task2 review/fix and production-toolchain evidence cover shared transport; Task1 provenance/package review covers source identity. Final SDK/container and full vet remain explicitly Task5 deliverables, not Task3 claims.
- Task 4: protocol_task4 implementing from base1a6ae63ef3f3337037b2db064f2b602a99f66e46; retains public/auth/context APIs, replaces legacy text transport with shared Serve and adds0010 plaintext/mixed-history patch. Task5 owns final SDK/container evidence.
- Ruling: Task4 rejects explicit Anthropic tool_choice with a field-path400 before Handler because the pinned direct Claude-to-Chat mapper does not map it (only Claude-to-Responses does). Keep the approved direct route and0010 history-only fix; do not silently lose semantics or introduce a second mapping chain. Cost/limit: forced/named Anthropic tool choice is unavailable and Task5 must document it; model-selected function tools remain supported.
- Task4 regression preservation: existing Anthropic malformed-output cases exposed shared completion acceptance of legacy function_call/refusal and empty null-content replies. Task4 may minimally fix shared validation with both-format RED/GREEN, retaining legitimate tool/reasoning-only replies. Requested-model Anthropic output contract stays in host normalization; no Responses model change or expanded library patch.
- Task4 implemented in e7630fa5b3c92349eacef2a7f0a418d430f97b8c; protocol_review4 reviewing full1a6ae63..e7630fa. Required entry/validator/history/completion RED-GREEN and core/library test-vet-race passed; no integration claim yet.
- Task4 review protocol_review4: three Important findings; fix round1 assigned to protocol_task4: empty reasoning must not validate absent output, thinking.display intent is not serialized and must reject, ordinary Claude malformed/nonobject arguments must not become fabricated{}. Preserve Responses raw argument behavior; test all three RED/GREEN before scoped rereview.
- Task4 fix round1 implemented in e9e5d65895adf98e8343501384a54fa6e43934d8: output-presence matrix, explicit visibility rejection, ordinary Claude argument-object validation including float overflow; RED/GREEN and four core package race passed. protocol_review4_fix1 scoped review running.
- Task4 fix round1/5 (3 addressed, 0 open; e9e5d65; protocol_review4_fix1 clean). Task4 complete (e7630fa + e9e5d65; spec compliance/quality approved). Cross-task source/package checks remain backed by Task1; bridge-only initialization by Task3; full owner/CSRF/SDK/container gate remains Task5 and must actually pass.
