# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:hy4-preview | responses | text | pass | 200 | 2897 ms |
| global:hy4-preview | responses | stream | pass | 200 | 20783 ms |
| global:hy4-preview | responses | reasoning_observed | pass |  |  ms |
| global:hy4-preview | responses | usage_observed | pass |  |  ms |
| global:hy4-preview | responses | history | pass | 200 | 3661 ms |
| global:hy4-preview | responses | tool_call | pass | 200 | 3454 ms |
| global:hy4-preview | responses | tool_result | pass | 200 | 11191 ms |
| global:hy4-preview | responses | strict_schema_conflict | fail | 200 | 24019 ms |
| global:hy4-preview | responses | image | pass | 200 | 1524 ms |
| global:hy4-preview | responses | inline_text_file | pass | 200 | 12978 ms |
| global:hy4-preview | anthropic | text | pass | 200 | 3261 ms |
| global:hy4-preview | anthropic | stream | pass | 200 | 3138 ms |
| global:hy4-preview | anthropic | reasoning_observed | pass |  |  ms |
| global:hy4-preview | anthropic | usage_observed | pass |  |  ms |
| global:hy4-preview | anthropic | history | pass | 200 | 5958 ms |
| global:hy4-preview | anthropic | tool_call | pass | 200 | 2557 ms |
| global:hy4-preview | anthropic | tool_result | pass | 200 | 2475 ms |
| global:hy4-preview | anthropic | strict_schema_conflict | skipped |  |  ms |
| global:hy4-preview | anthropic | image | pass | 200 | 1536 ms |
| global:hy4-preview | anthropic | inline_text_file | pass | 200 | 4556 ms |
