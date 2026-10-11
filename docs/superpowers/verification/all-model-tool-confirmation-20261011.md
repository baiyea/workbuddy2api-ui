# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| space-bunny | responses | tool_call | pass | 200 | 1964 ms |
| space-bunny | responses | tool_result | fail | 200 | 2980 ms |
| space-bunny | anthropic | tool_call | pass | 200 | 2100 ms |
| space-bunny | anthropic | tool_result | pass | 200 | 1907 ms |
