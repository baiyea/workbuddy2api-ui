# 三模型真实能力测试（2026-10-11）

已建立 `scripts/probe_model_capabilities.py`，使用 Python 标准库，接受重复的 `--model 名称=完整ID`。测试通过隔离 console → core → 真实 WorkBuddy；使用用户已登录账号的副本，关闭定时任务。结果是当前账号、模型ID及转换链路在这些用例中的观测，不代表完整 OpenAI/Anthropic 兼容。

| 测试功能 | DeepSeek | GLM | Kimi |
|---|---|---|---|
| 模型ID | `cn:deepseek-v4.1-flash` | `cn:glm-5.3` | `cn:kimi-k2.8-preview` |
| Responses / Messages 普通文本 | 两者通过 | 两者通过 | 两者通过 |
| Responses / Messages SSE 流式 | 两者通过 | 两者通过 | 两者通过 |
| 返回推理内容与真实用量 | 两者观察到 | 两者观察到 | 两者观察到 |
| 客户端提供完整多轮历史 | 两者通过 | 两者通过 | 两者通过 |
| 流式工具参数、调用ID与工具结果回填 | 两者通过 | 两者通过 | 两者通过 |
| 内联 PNG 图片颜色识别 | 两者通过 | 两者通过 | 两者通过 |
| 内联文本附件提取 | 两者通过 | 两者通过 | 两者通过 |
| Responses strict JSON Schema 冲突提示 | 未遵守 | 未遵守 | 未遵守 |
| Messages 输出 Schema | 网关明确不支持，跳过 | 网关明确不支持，跳过 | 网关明确不支持，跳过 |

最终有效矩阵45次真实请求：42次功能断言通过，3次Schema断言失败（均HTTP200、正常完成）；另有12项复用流式响应的推理/用量观测通过、3项Messages Schema跳过。没有执行真实工具。早期失败诊断和脚本中断前的重复调用另行保留，不计入这45次矩阵。

Schema要求精确输出 `{"marker":"schema_only_47","count":7}`，用户提示要求普通中文、不要JSON。三个响应都为普通中文，如DeepSeek返回“这是一句用于测试的普通中文说明，请确认你能正常收到。”因此只能判定当前链路没有强制遵守Schema，不能推断这些模型的原生供应商接口永远不支持Schema。脚本按设计退出1并保存失败证据。

## 使用方法

先在当前终端设置 `WB2A_API_KEY`，或提供包含 `api_key` 字段的本地JSON文件。PowerShell可直接执行一行：

```powershell
python scripts/probe_model_capabilities.py --base-url http://127.0.0.1:7863/v1 --model "DeepSeek=cn:deepseek-v4.1-flash" --model "GLM=cn:glm-5.3" --model "Kimi=cn:kimi-k2.8-preview" --output docs/superpowers/verification/my-model-results.json
```

默认逐项测试；可用 `--checks text stream history tools schema image inline_file` 选择子集。请求串行、脚本不重试，设置超时/输出token预算；基线失败会停止该协议后续测试，并至多用Chat做一次诊断。JSON保留请求、响应与断言结果，旁边生成Markdown表；密钥不写报告，已有任一报告都需要显式 `--overwrite` 才覆盖。

## 修复及验证

首轮三个模型的两个转换入口均502，原Chat成功。真实原始流包含WorkBuddy空序列化占位字段及非终止空finish。`5a6db9a` 在共享协议层仅清理已观察到的无语义空值，保留非空/未知字段、非法结束、大小限制及取消校验；不改原Chat或冻结上游。

- 完整Handler文本/工具JSON与SSE回归先RED后GREEN；五个相关Go包test/vet/race通过。
- 从精确LF源码5a6db9a构建新Linux amd64 core，镜像构建内RelayKit test/vet、core全套test和二进制构建通过；使用原已验收console镜像。
- 脚本发现并修复Schema参数名冲突、SSE服务错误分类、报告覆盖保护和基线失败配额保护；36ce295的10项定点回归通过，独立复审四项全部关闭。
- 同36ce295源码Linux脚本测试54项全部通过，见 [完整输出](real-model-linux-script-tests.txt)。Windows原生运行这套旧overlay/release测试有路径、符号链接、权限及Unix环境假设失败，未修改无关业务来隐藏；本次新脚本10项在Windows原生通过。
- 临时两服务已停止、原始凭据SHA256一致。没有发布、推送、部署或重启用户实际服务；用户已有`.gitignore`、`upstream-convert/`及旧探测材料保留。

## 原始证据

- [DeepSeek逐项表](real-model-capabilities-20261011-deepseek.md) / [JSON](real-model-capabilities-20261011-deepseek.json)
- [GLM与Kimi逐项表](real-model-capabilities-20261011-glm-kimi.md) / [JSON](real-model-capabilities-20261011-glm-kimi.json)
- [Schema逐项表](real-model-capabilities-20261011-schema.md) / [JSON](real-model-capabilities-20261011-schema.json)
- [镜像、源码身份及证据SHA256](real-model-capabilities-20261011-summary.json)
- [修复前六个502](real-model-capabilities-20261011.json)、[首次脚本中断前的已通过部分](real-model-capabilities-20261011-fixed.json)均保留。

未覆盖所有模型、长上下文、PDF附件、精确推理预算、并行/强制工具语义、文件存储、状态化Responses、真实SDK客户端全流程或原生安装器。推理内容观测不证明预算控制；简单图片与文本附件通过不证明所有媒体格式支持。严格Schema不做生成后修复，这是此前确认的转换层范围。
