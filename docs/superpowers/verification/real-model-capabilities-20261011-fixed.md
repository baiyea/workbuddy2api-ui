# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| DeepSeek | responses | text | pass | 200 | 2102 ms |
| DeepSeek | responses | stream | pass | 200 | 1878 ms |
| DeepSeek | responses | reasoning_observed | pass |  |  ms |
| DeepSeek | responses | usage_observed | pass |  |  ms |
| DeepSeek | responses | history | pass | 200 | 2104 ms |
| DeepSeek | responses | tool_call | pass | 200 | 2081 ms |
| DeepSeek | responses | tool_result | pass | 200 | 2152 ms |
