# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| DeepSeek | responses | strict_schema_conflict | fail | 200 | 2387 ms |
| DeepSeek | anthropic | strict_schema_conflict | skipped |  |  ms |
| GLM | responses | strict_schema_conflict | fail | 200 | 3237 ms |
| GLM | anthropic | strict_schema_conflict | skipped |  |  ms |
| Kimi | responses | strict_schema_conflict | fail | 200 | 3083 ms |
| Kimi | anthropic | strict_schema_conflict | skipped |  |  ms |
