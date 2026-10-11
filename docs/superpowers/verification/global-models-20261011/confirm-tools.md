# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:gpt-5.3-codex | responses | tool_call | pass | 200 | 5540 ms |
| global:gpt-5.3-codex | responses | tool_result | pass | 200 | 3697 ms |
| global:gpt-5.3-codex | anthropic | tool_call | pass | 200 | 5712 ms |
| global:gpt-5.3-codex | anthropic | tool_result | pass | 200 | 2716 ms |
| global:gemini-3.5-flash | responses | tool_call | error | 502 | 1960 ms |
| global:gemini-3.5-flash | anthropic | tool_call | error | 502 | 2356 ms |
