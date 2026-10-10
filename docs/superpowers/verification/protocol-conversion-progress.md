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
