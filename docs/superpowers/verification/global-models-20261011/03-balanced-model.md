# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:balanced-model | responses | text | pass | 200 | 1655 ms |
| global:balanced-model | responses | stream | pass | 200 | 1478 ms |
| global:balanced-model | responses | reasoning_observed | not_observed |  |  ms |
| global:balanced-model | responses | usage_observed | pass |  |  ms |
| global:balanced-model | responses | history | pass | 200 | 1622 ms |
| global:balanced-model | responses | tool_call | pass | 200 | 2154 ms |
| global:balanced-model | responses | tool_result | pass | 200 | 1718 ms |
| global:balanced-model | responses | strict_schema_conflict | fail | 200 | 2244 ms |
| global:balanced-model | responses | image | pass | 200 | 1705 ms |
| global:balanced-model | responses | inline_text_file | pass | 200 | 1988 ms |
| global:balanced-model | anthropic | text | pass | 200 | 1637 ms |
| global:balanced-model | anthropic | stream | pass | 200 | 1452 ms |
| global:balanced-model | anthropic | reasoning_observed | not_observed |  |  ms |
| global:balanced-model | anthropic | usage_observed | pass |  |  ms |
| global:balanced-model | anthropic | history | pass | 200 | 1849 ms |
| global:balanced-model | anthropic | tool_call | pass | 200 | 2361 ms |
| global:balanced-model | anthropic | tool_result | pass | 200 | 1736 ms |
| global:balanced-model | anthropic | strict_schema_conflict | skipped |  |  ms |
| global:balanced-model | anthropic | image | pass | 200 | 1627 ms |
| global:balanced-model | anthropic | inline_text_file | pass | 200 | 1651 ms |
