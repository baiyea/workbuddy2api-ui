# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:primary-model | responses | text | pass | 200 | 4001 ms |
| global:primary-model | responses | stream | pass | 200 | 5520 ms |
| global:primary-model | responses | reasoning_observed | not_observed |  |  ms |
| global:primary-model | responses | usage_observed | pass |  |  ms |
| global:primary-model | responses | history | pass | 200 | 3901 ms |
| global:primary-model | responses | tool_call | pass | 200 | 3347 ms |
| global:primary-model | responses | tool_result | pass | 200 | 2570 ms |
| global:primary-model | responses | strict_schema_conflict | fail | 200 | 4643 ms |
| global:primary-model | responses | image | pass | 200 | 4273 ms |
| global:primary-model | responses | inline_text_file | pass | 200 | 3705 ms |
| global:primary-model | anthropic | text | pass | 200 | 4466 ms |
| global:primary-model | anthropic | stream | pass | 200 | 3573 ms |
| global:primary-model | anthropic | reasoning_observed | not_observed |  |  ms |
| global:primary-model | anthropic | usage_observed | pass |  |  ms |
| global:primary-model | anthropic | history | pass | 200 | 5275 ms |
| global:primary-model | anthropic | tool_call | pass | 200 | 3910 ms |
| global:primary-model | anthropic | tool_result | pass | 200 | 2858 ms |
| global:primary-model | anthropic | strict_schema_conflict | skipped |  |  ms |
| global:primary-model | anthropic | image | pass | 200 | 5868 ms |
| global:primary-model | anthropic | inline_text_file | pass | 200 | 5095 ms |
