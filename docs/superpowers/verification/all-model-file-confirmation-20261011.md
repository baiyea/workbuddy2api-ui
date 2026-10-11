# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| GLM Flash | responses | inline_text_file | fail | 200 | 9271 ms |
| GLM Flash | anthropic | inline_text_file | fail | 200 | 3966 ms |
| MiniMax | responses | inline_text_file | pass | 200 | 1705 ms |
| MiniMax | anthropic | inline_text_file | pass | 200 | 1932 ms |
