# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:gpt-6-astra | responses | stream | pass | 200 | 2986 ms |
| global:gpt-6-astra | responses | reasoning_observed | not_observed |  |  ms |
| global:gpt-6-astra | responses | usage_observed | pass |  |  ms |
| global:gpt-6-astra | responses | tool_call | pass | 200 | 3461 ms |
| global:gpt-6-astra | responses | tool_result | pass | 200 | 3864 ms |
| global:gpt-6-astra | responses | image | pass | 200 | 3779 ms |
| global:gpt-6-astra | responses | inline_text_file | error | 502 | 1287 ms |
| global:gpt-6-astra | anthropic | stream | error | 502 | 1239 ms |
| global:gpt-6-astra | anthropic | reasoning_observed | not_observed |  |  ms |
| global:gpt-6-astra | anthropic | usage_observed | not_observed |  |  ms |
| global:gpt-6-astra | anthropic | tool_call | error | 502 | 1241 ms |
| global:gpt-6-astra | anthropic | image | pass | 200 | 7213 ms |
| global:gpt-6-astra | anthropic | inline_text_file | pass | 200 | 4426 ms |
