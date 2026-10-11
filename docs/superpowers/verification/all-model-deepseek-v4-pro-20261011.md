# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| DeepSeek V4 Pro | responses | text | pass | 200 | 2217 ms |
| DeepSeek V4 Pro | responses | stream | pass | 200 | 2385 ms |
| DeepSeek V4 Pro | responses | reasoning_observed | pass |  |  ms |
| DeepSeek V4 Pro | responses | usage_observed | pass |  |  ms |
| DeepSeek V4 Pro | responses | history | pass | 200 | 2264 ms |
| DeepSeek V4 Pro | responses | tool_call | pass | 200 | 2320 ms |
| DeepSeek V4 Pro | responses | tool_result | pass | 200 | 2639 ms |
| DeepSeek V4 Pro | responses | strict_schema_conflict | fail | 200 | 3274 ms |
| DeepSeek V4 Pro | responses | image | pass | 200 | 2059 ms |
| DeepSeek V4 Pro | responses | inline_text_file | pass | 200 | 2682 ms |
| DeepSeek V4 Pro | anthropic | text | pass | 200 | 1938 ms |
| DeepSeek V4 Pro | anthropic | stream | pass | 200 | 2389 ms |
| DeepSeek V4 Pro | anthropic | reasoning_observed | pass |  |  ms |
| DeepSeek V4 Pro | anthropic | usage_observed | pass |  |  ms |
| DeepSeek V4 Pro | anthropic | history | pass | 200 | 2451 ms |
| DeepSeek V4 Pro | anthropic | tool_call | pass | 200 | 2340 ms |
| DeepSeek V4 Pro | anthropic | tool_result | pass | 200 | 1944 ms |
| DeepSeek V4 Pro | anthropic | strict_schema_conflict | skipped |  |  ms |
| DeepSeek V4 Pro | anthropic | image | pass | 200 | 1695 ms |
| DeepSeek V4 Pro | anthropic | inline_text_file | pass | 200 | 2351 ms |
