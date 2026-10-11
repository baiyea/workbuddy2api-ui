# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:fast-model | responses | text | pass | 200 | 1511 ms |
| global:fast-model | responses | stream | pass | 200 | 2206 ms |
| global:fast-model | responses | reasoning_observed | not_observed |  |  ms |
| global:fast-model | responses | usage_observed | pass |  |  ms |
| global:fast-model | responses | history | pass | 200 | 1619 ms |
| global:fast-model | responses | tool_call | pass | 200 | 1657 ms |
| global:fast-model | responses | tool_result | pass | 200 | 2034 ms |
| global:fast-model | responses | strict_schema_conflict | fail | 200 | 1643 ms |
| global:fast-model | responses | image | pass | 200 | 2435 ms |
| global:fast-model | responses | inline_text_file | pass | 200 | 2036 ms |
| global:fast-model | anthropic | text | pass | 200 | 1626 ms |
| global:fast-model | anthropic | stream | pass | 200 | 1590 ms |
| global:fast-model | anthropic | reasoning_observed | not_observed |  |  ms |
| global:fast-model | anthropic | usage_observed | pass |  |  ms |
| global:fast-model | anthropic | history | pass | 200 | 3572 ms |
| global:fast-model | anthropic | tool_call | pass | 200 | 4805 ms |
| global:fast-model | anthropic | tool_result | pass | 200 | 1900 ms |
| global:fast-model | anthropic | strict_schema_conflict | skipped |  |  ms |
| global:fast-model | anthropic | image | pass | 200 | 2651 ms |
| global:fast-model | anthropic | inline_text_file | pass | 200 | 1827 ms |
