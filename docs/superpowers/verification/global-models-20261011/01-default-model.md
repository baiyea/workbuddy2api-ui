# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:default-model | responses | text | pass | 200 | 1894 ms |
| global:default-model | responses | stream | pass | 200 | 1463 ms |
| global:default-model | responses | reasoning_observed | not_observed |  |  ms |
| global:default-model | responses | usage_observed | pass |  |  ms |
| global:default-model | responses | history | pass | 200 | 1798 ms |
| global:default-model | responses | tool_call | pass | 200 | 2111 ms |
| global:default-model | responses | tool_result | pass | 200 | 1613 ms |
| global:default-model | responses | strict_schema_conflict | fail | 200 | 1833 ms |
| global:default-model | responses | image | pass | 200 | 2585 ms |
| global:default-model | responses | inline_text_file | fail | 200 | 1623 ms |
| global:default-model | anthropic | text | pass | 200 | 1848 ms |
| global:default-model | anthropic | stream | pass | 200 | 1512 ms |
| global:default-model | anthropic | reasoning_observed | not_observed |  |  ms |
| global:default-model | anthropic | usage_observed | pass |  |  ms |
| global:default-model | anthropic | history | pass | 200 | 1729 ms |
| global:default-model | anthropic | tool_call | pass | 200 | 2086 ms |
| global:default-model | anthropic | tool_result | pass | 200 | 1481 ms |
| global:default-model | anthropic | strict_schema_conflict | skipped |  |  ms |
| global:default-model | anthropic | image | pass | 200 | 4365 ms |
| global:default-model | anthropic | inline_text_file | fail | 200 | 1698 ms |
