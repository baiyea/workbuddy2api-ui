# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:gemini-3.5-flash | responses | text | pass | 200 | 2395 ms |
| global:gemini-3.5-flash | responses | stream | pass | 200 | 3031 ms |
| global:gemini-3.5-flash | responses | reasoning_observed | not_observed |  |  ms |
| global:gemini-3.5-flash | responses | usage_observed | pass |  |  ms |
| global:gemini-3.5-flash | responses | history | pass | 200 | 2517 ms |
| global:gemini-3.5-flash | responses | tool_call | error | 502 | 2423 ms |
| global:gemini-3.5-flash | responses | strict_schema_conflict | fail | 200 | 4482 ms |
| global:gemini-3.5-flash | responses | image | pass | 200 | 8823 ms |
| global:gemini-3.5-flash | responses | inline_text_file | pass | 200 | 3157 ms |
| global:gemini-3.5-flash | anthropic | text | pass | 200 | 2663 ms |
| global:gemini-3.5-flash | anthropic | stream | pass | 200 | 2237 ms |
| global:gemini-3.5-flash | anthropic | reasoning_observed | not_observed |  |  ms |
| global:gemini-3.5-flash | anthropic | usage_observed | pass |  |  ms |
| global:gemini-3.5-flash | anthropic | history | pass | 200 | 2513 ms |
| global:gemini-3.5-flash | anthropic | tool_call | error | 502 | 2667 ms |
| global:gemini-3.5-flash | anthropic | strict_schema_conflict | skipped |  |  ms |
| global:gemini-3.5-flash | anthropic | image | pass | 200 | 2894 ms |
| global:gemini-3.5-flash | anthropic | inline_text_file | pass | 200 | 2987 ms |
