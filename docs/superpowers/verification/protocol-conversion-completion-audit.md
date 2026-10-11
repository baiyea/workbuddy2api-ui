# 协议转换交付核验

当前状态：最终整体评审发现的 7 个重要问题已修复并通过范围限定的独立复审；本地回归、竞态及双 SDK 原生测试通过。Docker Desktop 已退出，用户选择自行启动后通知；新源码的 Linux 全量检查、新镜像及实际 console/core/mock 双 SDK 容器验收仍待执行。交付门槛尚未通过，不能将本记录解读为功能已经完成、发布、部署或真实模型能力验证。

| 目标 / 约束 | 权威证据 | 当前结论 |
| --- | --- | --- |
| 先设计、计划、确认，再子代理实施 | specs/plans 2026-10-10 文档、progress.md 已确认实施阶段、各任务提交与评审 | 已完成 |
| 固定复用 RelayKit，不另造转换器 | SOURCE.json；final-source-integrity.json 的 162 文件与固定 Git blob 核验；公开转换入口；0008–0011 补丁 | 已完成 |
| 原始冻结快照与库来源保持 | baseline 8cbf7ae 至 e213c4b 的 upstream/upstream.lock diff 为空；vendor 原始文件哈希全部一致 | 已完成 |
| 同两个服务、账号池、Handler、一键部署 | 生产 Compose/config diff 为空；Task3 bridge/no-bridge wiring 回归；实际隔离启动/重建 | 已完成 |
| Responses 无状态入口与工具/明文历史 | Task3 回归；final-fix-report.md / final-fix-rereview.md 的真实 Handler 及双 SDK 出站断言 | 修复及本地验证通过，待新镜像验收 |
| Anthropic 增强及混合历史 | Task4 回归；最终修复的完整工具 JSON、并行和交错片段 SDK 断言 | 修复及本地验证通过，待新镜像验收 |
| 用量、结束、大小与取消 | 最终修复真实 Handler RED/GREEN，原始流限额/终态/部分用量矩阵及独立复审 | 修复及本地验证通过，待新镜像验收 |
| JSON Schema 转换后确实到达上游 | task-5-gateway-final-logs/acceptance.log：JSON/SSE 两种 full Handler payload 精确断言 | 已完成；不保证模型结果符合 schema |
| 不增加 Files 存储或生成结果修复 | 设计与 README 边界、路径/字段拒绝回归、补丁范围 | 已完成；最终整体审查继续核对 |
| 公共/内部/管理认证、owner、CSRF、同源 | Task2–4 回归；最终管理 auth/CSRF/origin/trusted ID/分 owner 退出验收 | 已完成 |
| SDK 实际网关而非仅 fixture | Python3.12.15，Anthropic0.67.0、OpenAI2.6.1、httpx0.28.1；最终 actual gateway 两 SDK PASS | 已完成 |
| SDK 高层 Responses 流聚合 | 实际 IndexError RED，0011 库 RED/GREEN，最终高层流聚合 PASS | 已完成 |
| 完整检查 | e73cc7a scripts44/deploy14/desktop11；check.sh 核心/库 test-vet、9 核心包和 console race、Node33、任务 Python15；Compose config | 全部 exit0 |
| 隔离容器验收 | ad1d61e 复用 exact e73 镜像，身份核对；fresh/rebuild/legacy + 两 SDK + gateway helper | exit0 |
| 随包声明 | 实际 core 镜像读取四文件/哈希；desktop 真实复制函数回归；控制台已有页脚署名 | 已完成 |
| README/AGENTS 能力边界 | Task5 文档 diff 与独立评审，明确不完整兼容和库/模型限制 | 已完成 |
| 全部任务逐项评审 | Task1–5 reports/reviews/fix reviews；Task5 仅一个非阻断覆盖改进建议 | 已完成 |
| 最终整个改动评审、处理建议 | final-review.md、final-fix-report.md、final-fix-rereview.md | 7 个重要问题及 1 个覆盖建议均 ADDRESSED，无新增重要问题 |
| 保留用户改动 / 不发布部署 | 当前只余用户 .gitignore 与原有未跟踪 probe/deferred/upstream-convert；所有实际服务保持；未 push/publish/deploy | 已核对，交付前再核对 |

## 验证版本说明

生产源码与完整 Go/Linux 检查版本为 e73cc7a；ad1d61e 仅改网关验收的一行精确期望，4d73a51 与 e213c4b 仅整理报告/评审。最终隔离验收使用既有机制复用精确镜像并核对 runtime identity，不声称重新构建了 ad1 源码。详细日志和镜像 ID 见 ../implementation/protocol-conversion/task-5-report.md。

以上是最终修复前的 Linux/容器证据，不能证明修复后的集成完成。修复生产提交为 851130d，测试和说明补充后复审 HEAD 为 3f4e6be。新本地证据见 final-fix-report.md；最终新镜像验收仍未运行。final-fix-source-integrity.json 再次确认 162 个库原始文件哈希匹配，冻结快照与生产 Compose 无变动。

## 代为裁定及边界

| 裁定 | 理由与若判断错误的代价 |
| --- | --- |
| 0008 补齐实际转换依赖的间接模块 | 空注册导入未覆盖完整依赖图；若遗漏则真实构建失败，已用 readonly 与生产工具链验证 |
| Task2 暂拒绝 reasoning，Task3 补丁后开放 | 避免中间阶段静默丢历史；最终已开放经校验明文，若遗留会影响多轮，已回归 |
| 仅允许 reasoning 序列化器产生的空值字段 | 原样 output 回填需要；若放宽过度可能吞语义，测试限定确切空值 |
| 明确拒绝 Anthropic tool_choice | 固定直接 mapper 不转换，避免忽略强制选择；代价是该功能在此路线不可用，README 已说明 |
| 持久化检查改为状态语义 + 凭据密钥字节一致 | 成功计数本来会变；若覆盖不足会漏状态损失，保留所有种子值及更新后的成功时间/次数 |
| 0011 补齐文本 content_part 生命周期 | 实际 SDK 聚合失败必须修；可能影响事件/DTO 消费者，已做库/core 与实际 SDK 验收 |
| 精确 schema 期望包含库保留的嵌套 type | 复用固定 mapper 原样语义；证明当前链路精确传递，不证明其他供应商接受该额外元数据 |
| 最终修复在可信 adapter 请求中启用严格原始流处理，并补齐有界内联媒体解析 | 必须在原 Handler 丢失信息或分配过量内存之前校验；错误接线可能改变原 Chat 行为，须以完整 Handler、未启用桥接路径和新镜像 SDK 验收证明。内联解析不增加文件存储或远程抓取 |

未进行真实上游再次探测、实际服务重启、推送、镜像发布、部署、Windows Docker 工作区构建或原生安装器验收。模拟验收不证明真实模型能力；JSON Schema 与推理/工具能力仍受上游限制。

## 最终评审未纳入项目的核对

已逐项核对 final-review.md 的 “Declined to judge”：真实模型能力归上游；持久化 Responses、Files 存储、后台工具、WebSocket 和生成 JSON 修复均在已确认范围之外；精确 thinking 预算、签名/加密推理及未支持的 Claude 强制工具选择等继续明确拒绝或注明近似。远程媒体抓取不纳入内联解析。原 Chat 的宽松处理保持，新增严格处理仅用于可信适配请求。没有增加任意供应商扩展的完整兼容承诺。

原生安装器/签名与跨平台实际运行、Windows 工作区 Docker 打包不作为本次 Linux 协议集成通过的替代证明；发布、推送、部署和实际服务重启没有授权。许可核对范围是来源、声明随包和署名实现，不是法律意见。这些排除项不构成推迟 7 个已复现问题的理由。
