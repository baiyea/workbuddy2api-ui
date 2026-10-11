# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:kimi-k3 | responses | text | pass | 200 | 4324 ms |
| global:kimi-k3 | responses | stream | pass | 200 | 4230 ms |
| global:kimi-k3 | responses | reasoning_observed | pass |  |  ms |
| global:kimi-k3 | responses | usage_observed | pass |  |  ms |
| global:kimi-k3 | responses | history | pass | 200 | 3457 ms |
| global:kimi-k3 | responses | tool_call | pass | 200 | 5835 ms |
| global:kimi-k3 | responses | tool_result | pass | 200 | 4182 ms |
| global:kimi-k3 | responses | strict_schema_conflict | fail | 200 | 7481 ms |
| global:kimi-k3 | responses | image | pass | 200 | 7308 ms |
| global:kimi-k3 | responses | inline_text_file | pass | 200 | 4092 ms |
| global:kimi-k3 | anthropic | text | pass | 200 | 5358 ms |
| global:kimi-k3 | anthropic | stream | pass | 200 | 4968 ms |
| global:kimi-k3 | anthropic | reasoning_observed | pass |  |  ms |
| global:kimi-k3 | anthropic | usage_observed | pass |  |  ms |
| global:kimi-k3 | anthropic | history | pass | 200 | 5737 ms |
| global:kimi-k3 | anthropic | tool_call | pass | 200 | 5908 ms |
| global:kimi-k3 | anthropic | tool_result | pass | 200 | 4472 ms |
| global:kimi-k3 | anthropic | strict_schema_conflict | skipped |  |  ms |
| global:kimi-k3 | anthropic | image | pass | 200 | 6668 ms |
| global:kimi-k3 | anthropic | inline_text_file | pass | 200 | 5550 ms |
