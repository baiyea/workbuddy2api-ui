# Gemini / Astra 定点重测（2026-10-11）

复用上一轮相同 core / console 镜像、通用能力脚本和国际账号副本；不修改业务代码。关闭六类定时任务，使用临时容器及随机本机端口。本轮 25 次网关请求，另有原始返回诊断，计数不等于上游计费次数。

| 模型 | 本轮项目 | Responses | Messages |
|---|---|---|---|
| `global:gpt-6-astra` | 流式文本 | 2/2 通过 | 2/2 通过 |
| 同上 | 流式工具调用 | 2/2 通过 | 2/2 通过 |
| 同上 | 工具结果回填 | 2/2 通过 | 2/2 通过 |
| 同上 | 图片辨色 | 2/2 通过 | 2/2 通过 |
| 同上 | 内联文本附件 | 2/2 通过 | 2/2 通过 |
| `global:gemini-3.5-flash` | 流式工具调用 | 502 | 502 |
| 同上 | 非流式工具调用 | 502 | 502 |

Astra 共 20 次请求全部 HTTP 200，内容、工具名/ID/参数和回填断言全部通过。这次没有复现历史 502，不能证明已修复或长期稳定；历史记录保留。本轮没有重新测试 Schema、多轮记忆和全部其他模型。

Gemini 原 `/v1/chat/completions` 流式对照 HTTP 200，返回完整 `probe_echo` 工具名、ID、正确参数及 `[DONE]`。两种转换接口各一次流式、一次非流式均 502；回填未执行。

## Gemini 失败点

原始上游工具 SSE 带有非空 `delta.extra_fields.reasoning_detail.toolcall_signature`。共享层 `normalizeEmptyDelta` 仅处理 `extra_fields:null`，保留非空对象；随后严格解码到 RelayKit delta DTO，触发 `json: unknown field "extra_fields"`，由适配器返回 502。非流式入口也先校验上游 SSE，所以同样受影响。

对实际捕获内容的离线回放，在 Responses / Messages 两种格式下均得到 `invalid delta`。仅在离线对照中删除该字段后，两者的流校验都通过，定位了此样本的拒绝点；没有改生产转换逻辑，也没有证明丢弃签名后多轮工具交互安全可用。此结果说明现有签名元数据兼容边界导致失败，不能标为模型缺少工具能力。

原始捕获使用现有 `real-protocol-capture-20261011_test.go` 的临时副本，增加 `client.GlobalEnabled = true`，以 Linux 测试二进制在现有 core 镜像内运行。最初复用国内捕获夹具时，Windows 原生网络连接失败，容器尝试国内端点返回 401；修正诊断夹具的地区开关后捕获成功。前两次是夹具/环境失败，不计入模型能力结果。生产网关始终使用正确的国际账号路由。

## 证据

- [Astra 第 1 轮](astra-round1.json)、[第 2 轮](astra-round2.json)：每轮 10 次真实请求。
- [Gemini 流式](gemini-round1.json)、[Chat 流式与转换非流式对照](gemini-controls.json)：共 5 次真实请求。
- [脱敏上游 SSE](gemini-raw-redacted.sse)、[原始内容摘要及签名长度](capture-metadata.json)：完整原始内容留在忽略的 `.build`，公开副本仅替换非空签名值。
- [离线回放夹具](raw_replay_test.go)、[原始样本结果](raw-replay.txt)、[脱敏样本结果](redacted-replay.txt)：两次均通过预期拒绝及字段删除对照断言。通过 Go test overlay 放入物化 core 的 `internal/protocol`，用 `PROBE_CAPTURE_FILE` 指定 SSE，运行 `go test -overlay <overlay.json> ./internal/protocol -run '^TestGeminiCapturedExtraFields$' -count=1 -v`。

测试容器及网络已停止并清理；两个原始账号文件 SHA256 均未改变。未发布、推送、部署或重启用户实际服务。未实现签名传递，也未验证 Gemini 工具回填；Astra 历史偶发 502 的根因仍未确定。
