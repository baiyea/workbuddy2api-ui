# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:gpt-6-astra | responses | text | pass | 200 | 4126 ms |
| global:gpt-6-astra | responses | stream | error | 502 | 1591 ms |
| global:gpt-6-astra | responses | reasoning_observed | not_observed |  |  ms |
| global:gpt-6-astra | responses | usage_observed | not_observed |  |  ms |
| global:gpt-6-astra | responses | history | pass | 200 | 3145 ms |
| global:gpt-6-astra | responses | tool_call | error | 502 | 1320 ms |
| global:gpt-6-astra | responses | strict_schema_conflict | fail | 200 | 4174 ms |
| global:gpt-6-astra | responses | image | error | 502 | 1207 ms |
| global:gpt-6-astra | responses | inline_text_file | error | 502 | 1201 ms |
| global:gpt-6-astra | anthropic | text | pass | 200 | 3417 ms |
| global:gpt-6-astra | anthropic | stream | pass | 200 | 5622 ms |
| global:gpt-6-astra | anthropic | reasoning_observed | pass |  |  ms |
| global:gpt-6-astra | anthropic | usage_observed | pass |  |  ms |
| global:gpt-6-astra | anthropic | history | pass | 200 | 2626 ms |
| global:gpt-6-astra | anthropic | tool_call | pass | 200 | 2852 ms |
| global:gpt-6-astra | anthropic | tool_result | pass | 200 | 2913 ms |
| global:gpt-6-astra | anthropic | strict_schema_conflict | skipped |  |  ms |
| global:gpt-6-astra | anthropic | image | pass | 200 | 4051 ms |
| global:gpt-6-astra | anthropic | inline_text_file | pass | 200 | 3703 ms |
