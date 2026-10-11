# API 接入与模型能力测试

[返回 README](../README.md#api-调用与范围) · [各模型支持情况](superpowers/verification/all-model-capability-table-20261011.md)

**我常用的 AI 工具能接入吗？**

先看它是否支持自定义服务地址，以及所需功能是否与下表匹配。工具由客户端执行，模型是否调用工具、理解媒体或返回推理内容取决于所选模型与上游。接口名称不代表完整 OpenAI、Anthropic 或 Claude Code 兼容；更多客户端的接入效果欢迎反馈实测结果。

## API 调用与范围

OpenAI SDK 的 Base URL 使用 `http://127.0.0.1:7863/v1`；Anthropic SDK 使用站点根地址 `http://127.0.0.1:7863`。使用 `/v1/models` 返回的完整模型 ID；curl 示例见 [README](../README.md#api-调用与范围)。

| 端点                                              | 当前能力与限制                                                                                                                                                                                                      |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /v1/models`、`POST /v1/chat/completions` | 现有模型列表、对话和 SSE 流式调用                                                                                                                                                                                   |
| `POST /v1/responses`                            | 无状态创建；文本/完整历史、function/custom 工具及结果回填、库可转换的图片/内联文件、明文 reasoning summary、JSON/SSE；`text.format` JSON 模式/JSON Schema 传递至上游，不校验或修复生成结果                        |
| `POST /v1/messages`（Beta）                     | 文本/历史、模型自主选择的工具及结果回填、HTTP(S) 图片或 PNG/JPEG/GIF/WebP base64、内联 text/plain 或 base64 PDF、无签名明文 thinking、JSON/SSE；`thinking.budget_tokens` 转为 effort，不能保证精确推理 token 预算 |

内联文件解析支持有界的 base64 text/JSON/PDF；不下载远程文件 URL。Claude 工具参数实时传送；工具开始后穿插的文本/思考会暂存到参数完整，再按顺序输出，避免丢失后续工具片段。

Responses 不提供 response 存储/检索/删除、后台执行、WebSocket 或 Files API。`previous_response_id`、`conversation`、模板 `prompt`、`context_management`、`store:true`、`background:true` 返回 400；多轮请发送完整历史。文件仓库引用、加密推理及无法转换的关键字段也会明确拒绝。

Messages 固定使用 `anthropic-version: 2023-06-01`，不接受 `anthropic-beta`、查询参数或压缩请求体。显式 `tool_choice`、工具 `strict`/服务端工具字段、cache/citations 元数据、非空 thinking 签名/redacted thinking、`thinking.display`、`tool_result.is_error:true`、`output_format` 及服务端状态字段返回带路径的 400；`output_config` 仅支持 effort。工具 input_schema 会传递，但 Messages 不提供 schema 输出约束。未知用量保持 `null`，不会估算或补零。

## 逐项测试模型能力

以下命令均从仓库根目录执行。

`scripts/probe_model_capabilities.py` 使用 Python 标准库，按模型名称和 `/v1/models` 中的完整 ID 测试 Responses / Messages。它会发起真实请求并消耗额度；串行运行、不自动重试、不实际执行工具。先将 API Key 放入当前终端的 `WB2A_API_KEY` 环境变量，再执行：

```bash
python scripts/probe_model_capabilities.py \
  --base-url http://127.0.0.1:7863/v1 \
  --model "DeepSeek=cn:deepseek-v4.1-flash" \
  --model "GLM=cn:glm-5.3" \
  --output docs/superpowers/verification/my-model-probe.json
```

国际区也复用此脚本，例如 `--model "GPT=global:gpt-5.6-sol"`；需要已登录对应国际账号。测试时使用 `/v1/models` 返回的完整 ID，保留国内 `cn:` 与国际 `global:` 前缀，避免混淆同名模型。

也可用 `--key-file 路径` 读取包含 `api_key` 的本地 JSON，密钥不会写进报告。`--output` 必须使用 `.json` 扩展名，结果同时保存 JSON 请求/响应证据与同名 Markdown 表格；任一报告已存在时默认拒绝覆盖，显式 `--overwrite` 才可覆盖两者。`--checks text stream history tools schema image inline_file` 可选择测试项，`--timeout` 和 `--max-output-tokens` 控制单次请求预算。PowerShell 可将示例写在一行执行。

文本基线未通过（包括完成但输出为空或标记不匹配）时会跳过该协议的后续调用，并用原 Chat 接口对每个模型最多做一次诊断，区分入口故障和模型可用性。`pass` / `fail` 是该用例的断言结果，`error` 表示 HTTP/传输/解析错误或 SSE 错误事件，`not_observed` 表示未观察到，不能直接推断“不支持”；`skipped` 会附原因。Schema 用例以冲突提示检查约束是否实际生效，一次通过也不保证严格遵守。Messages 的输出 Schema 当前由网关明确不支持，脚本标记跳过，不归因于模型。出现 `fail` 或 `error` 时脚本退出码为 1。
