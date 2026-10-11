# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:hy4-preview-f | responses | text | pass | 200 | 2972 ms |
| global:hy4-preview-f | responses | stream | pass | 200 | 4959 ms |
| global:hy4-preview-f | responses | reasoning_observed | pass |  |  ms |
| global:hy4-preview-f | responses | usage_observed | pass |  |  ms |
| global:hy4-preview-f | responses | history | pass | 200 | 11801 ms |
| global:hy4-preview-f | responses | tool_call | pass | 200 | 2849 ms |
| global:hy4-preview-f | responses | tool_result | pass | 200 | 2587 ms |
| global:hy4-preview-f | responses | strict_schema_conflict | fail | 200 | 10014 ms |
| global:hy4-preview-f | responses | image | pass | 200 | 2252 ms |
| global:hy4-preview-f | responses | inline_text_file | pass | 200 | 4113 ms |
| global:hy4-preview-f | anthropic | text | pass | 200 | 2536 ms |
| global:hy4-preview-f | anthropic | stream | pass | 200 | 5238 ms |
| global:hy4-preview-f | anthropic | reasoning_observed | pass |  |  ms |
| global:hy4-preview-f | anthropic | usage_observed | pass |  |  ms |
| global:hy4-preview-f | anthropic | history | pass | 200 | 2668 ms |
| global:hy4-preview-f | anthropic | tool_call | pass | 200 | 9040 ms |
| global:hy4-preview-f | anthropic | tool_result | pass | 200 | 2361 ms |
| global:hy4-preview-f | anthropic | strict_schema_conflict | skipped |  |  ms |
| global:hy4-preview-f | anthropic | image | pass | 200 | 1407 ms |
| global:hy4-preview-f | anthropic | inline_text_file | pass | 200 | 4860 ms |
