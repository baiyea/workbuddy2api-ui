# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| DeepSeek | responses | text | pass | 200 | 1948 ms |
| DeepSeek | responses | stream | pass | 200 | 1727 ms |
| DeepSeek | responses | reasoning_observed | pass |  |  ms |
| DeepSeek | responses | usage_observed | pass |  |  ms |
| DeepSeek | responses | history | pass | 200 | 2015 ms |
| DeepSeek | responses | tool_call | pass | 200 | 1536 ms |
| DeepSeek | responses | tool_result | pass | 200 | 1767 ms |
| DeepSeek | responses | image | pass | 200 | 2613 ms |
| DeepSeek | responses | inline_text_file | pass | 200 | 2077 ms |
| DeepSeek | anthropic | text | pass | 200 | 1571 ms |
| DeepSeek | anthropic | stream | pass | 200 | 2017 ms |
| DeepSeek | anthropic | reasoning_observed | pass |  |  ms |
| DeepSeek | anthropic | usage_observed | pass |  |  ms |
| DeepSeek | anthropic | history | pass | 200 | 1966 ms |
| DeepSeek | anthropic | tool_call | pass | 200 | 1995 ms |
| DeepSeek | anthropic | tool_result | pass | 200 | 1720 ms |
| DeepSeek | anthropic | image | pass | 200 | 1931 ms |
| DeepSeek | anthropic | inline_text_file | pass | 200 | 2094 ms |
