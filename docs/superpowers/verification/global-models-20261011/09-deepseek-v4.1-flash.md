# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:deepseek-v4.1-flash | responses | text | pass | 200 | 1780 ms |
| global:deepseek-v4.1-flash | responses | stream | pass | 200 | 1859 ms |
| global:deepseek-v4.1-flash | responses | reasoning_observed | pass |  |  ms |
| global:deepseek-v4.1-flash | responses | usage_observed | pass |  |  ms |
| global:deepseek-v4.1-flash | responses | history | pass | 200 | 1806 ms |
| global:deepseek-v4.1-flash | responses | tool_call | pass | 200 | 1872 ms |
| global:deepseek-v4.1-flash | responses | tool_result | pass | 200 | 1735 ms |
| global:deepseek-v4.1-flash | responses | strict_schema_conflict | fail | 200 | 1892 ms |
| global:deepseek-v4.1-flash | responses | image | pass | 200 | 1756 ms |
| global:deepseek-v4.1-flash | responses | inline_text_file | pass | 200 | 2308 ms |
| global:deepseek-v4.1-flash | anthropic | text | pass | 200 | 1704 ms |
| global:deepseek-v4.1-flash | anthropic | stream | pass | 200 | 1426 ms |
| global:deepseek-v4.1-flash | anthropic | reasoning_observed | pass |  |  ms |
| global:deepseek-v4.1-flash | anthropic | usage_observed | pass |  |  ms |
| global:deepseek-v4.1-flash | anthropic | history | pass | 200 | 2004 ms |
| global:deepseek-v4.1-flash | anthropic | tool_call | pass | 200 | 1799 ms |
| global:deepseek-v4.1-flash | anthropic | tool_result | pass | 200 | 1653 ms |
| global:deepseek-v4.1-flash | anthropic | strict_schema_conflict | skipped |  |  ms |
| global:deepseek-v4.1-flash | anthropic | image | pass | 200 | 2052 ms |
| global:deepseek-v4.1-flash | anthropic | inline_text_file | pass | 200 | 2419 ms |
