# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| gemini | responses | tool_call | error | 502 | 3350 ms |
| gemini | anthropic | tool_call | error | 502 | 3203 ms |
