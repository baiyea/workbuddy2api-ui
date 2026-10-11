# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:glm-5.2 | responses | image | fail | 200 | 2815 ms |
| global:glm-5.2 | anthropic | image | fail | 200 | 2225 ms |
| global:glm-5.3 | responses | image | fail | 200 | 3556 ms |
| global:glm-5.3 | anthropic | image | fail | 200 | 2932 ms |
