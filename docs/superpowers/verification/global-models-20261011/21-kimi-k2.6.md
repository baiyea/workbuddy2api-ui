# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| global:kimi-k2.6 | responses | text | pass | 200 | 1838 ms |
| global:kimi-k2.6 | responses | stream | pass | 200 | 8072 ms |
| global:kimi-k2.6 | responses | reasoning_observed | not_observed |  |  ms |
| global:kimi-k2.6 | responses | usage_observed | pass |  |  ms |
| global:kimi-k2.6 | responses | history | pass | 200 | 1816 ms |
| global:kimi-k2.6 | responses | tool_call | pass | 200 | 2341 ms |
| global:kimi-k2.6 | responses | tool_result | pass | 200 | 2171 ms |
| global:kimi-k2.6 | responses | strict_schema_conflict | fail | 200 | 2414 ms |
| global:kimi-k2.6 | responses | image | pass | 200 | 1657 ms |
| global:kimi-k2.6 | responses | inline_text_file | pass | 200 | 3195 ms |
| global:kimi-k2.6 | anthropic | text | pass | 200 | 2622 ms |
| global:kimi-k2.6 | anthropic | stream | pass | 200 | 2080 ms |
| global:kimi-k2.6 | anthropic | reasoning_observed | not_observed |  |  ms |
| global:kimi-k2.6 | anthropic | usage_observed | pass |  |  ms |
| global:kimi-k2.6 | anthropic | history | pass | 200 | 2049 ms |
| global:kimi-k2.6 | anthropic | tool_call | pass | 200 | 2712 ms |
| global:kimi-k2.6 | anthropic | tool_result | pass | 200 | 2146 ms |
| global:kimi-k2.6 | anthropic | strict_schema_conflict | skipped |  |  ms |
| global:kimi-k2.6 | anthropic | image | pass | 200 | 1926 ms |
| global:kimi-k2.6 | anthropic | inline_text_file | pass | 200 | 1835 ms |
