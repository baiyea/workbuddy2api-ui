# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| DeepSeek | responses | text | error | 502 | 1316 ms |
| DeepSeek | responses | remaining | skipped |  |  ms |
| DeepSeek | anthropic | text | error | 502 | 1539 ms |
| DeepSeek | anthropic | remaining | skipped |  |  ms |
| GLM | responses | text | error | 502 | 2021 ms |
| GLM | responses | remaining | skipped |  |  ms |
| GLM | anthropic | text | error | 502 | 1862 ms |
| GLM | anthropic | remaining | skipped |  |  ms |
| Kimi | responses | text | error | 502 | 1597 ms |
| Kimi | responses | remaining | skipped |  |  ms |
| Kimi | anthropic | text | error | 502 | 1632 ms |
| Kimi | anthropic | remaining | skipped |  |  ms |
