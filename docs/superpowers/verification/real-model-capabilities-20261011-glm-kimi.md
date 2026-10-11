# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| GLM | responses | text | pass | 200 | 2640 ms |
| GLM | responses | stream | pass | 200 | 2735 ms |
| GLM | responses | reasoning_observed | pass |  |  ms |
| GLM | responses | usage_observed | pass |  |  ms |
| GLM | responses | history | pass | 200 | 2499 ms |
| GLM | responses | tool_call | pass | 200 | 2943 ms |
| GLM | responses | tool_result | pass | 200 | 2614 ms |
| GLM | responses | image | pass | 200 | 2316 ms |
| GLM | responses | inline_text_file | pass | 200 | 4285 ms |
| GLM | anthropic | text | pass | 200 | 3518 ms |
| GLM | anthropic | stream | pass | 200 | 2641 ms |
| GLM | anthropic | reasoning_observed | pass |  |  ms |
| GLM | anthropic | usage_observed | pass |  |  ms |
| GLM | anthropic | history | pass | 200 | 2973 ms |
| GLM | anthropic | tool_call | pass | 200 | 4910 ms |
| GLM | anthropic | tool_result | pass | 200 | 5966 ms |
| GLM | anthropic | image | pass | 200 | 2222 ms |
| GLM | anthropic | inline_text_file | pass | 200 | 3120 ms |
| Kimi | responses | text | pass | 200 | 3530 ms |
| Kimi | responses | stream | pass | 200 | 3105 ms |
| Kimi | responses | reasoning_observed | pass |  |  ms |
| Kimi | responses | usage_observed | pass |  |  ms |
| Kimi | responses | history | pass | 200 | 3316 ms |
| Kimi | responses | tool_call | pass | 200 | 3900 ms |
| Kimi | responses | tool_result | pass | 200 | 2964 ms |
| Kimi | responses | image | pass | 200 | 3587 ms |
| Kimi | responses | inline_text_file | pass | 200 | 6133 ms |
| Kimi | anthropic | text | pass | 200 | 2991 ms |
| Kimi | anthropic | stream | pass | 200 | 3163 ms |
| Kimi | anthropic | reasoning_observed | pass |  |  ms |
| Kimi | anthropic | usage_observed | pass |  |  ms |
| Kimi | anthropic | history | pass | 200 | 3136 ms |
| Kimi | anthropic | tool_call | pass | 200 | 2639 ms |
| Kimi | anthropic | tool_result | pass | 200 | 2326 ms |
| Kimi | anthropic | image | pass | 200 | 3517 ms |
| Kimi | anthropic | inline_text_file | pass | 200 | 4085 ms |
