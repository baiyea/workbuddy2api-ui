# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:deep-model | responses | text | error | 503 | 956 ms |
| global:deep-model | chat | diagnostic_baseline | error | 503 | 533 ms |
| global:deep-model | responses | remaining | skipped |  |  ms |
| global:deep-model | anthropic | text | error | 503 | 436 ms |
| global:deep-model | anthropic | remaining | skipped |  |  ms |
