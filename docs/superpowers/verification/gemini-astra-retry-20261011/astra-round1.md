# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| astra | responses | stream | pass | 200 | 3747 ms |
| astra | responses | reasoning_observed | not_observed |  |  ms |
| astra | responses | usage_observed | pass |  |  ms |
| astra | responses | tool_call | pass | 200 | 3513 ms |
| astra | responses | tool_result | pass | 200 | 4958 ms |
| astra | responses | image | pass | 200 | 2987 ms |
| astra | responses | inline_text_file | pass | 200 | 5320 ms |
| astra | anthropic | stream | pass | 200 | 4125 ms |
| astra | anthropic | reasoning_observed | not_observed |  |  ms |
| astra | anthropic | usage_observed | pass |  |  ms |
| astra | anthropic | tool_call | pass | 200 | 2976 ms |
| astra | anthropic | tool_result | pass | 200 | 3595 ms |
| astra | anthropic | image | pass | 200 | 4144 ms |
| astra | anthropic | inline_text_file | pass | 200 | 5989 ms |
