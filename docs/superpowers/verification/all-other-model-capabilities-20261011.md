# 真实模型功能探测

一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。

| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |
|---|---|---|---|---|---|
| auto | responses | text | pass | 200 | 4635 ms |
| auto | responses | stream | pass | 200 | 5216 ms |
| auto | responses | reasoning_observed | pass |  |  ms |
| auto | responses | usage_observed | pass |  |  ms |
| auto | responses | history | pass | 200 | 3797 ms |
| auto | responses | tool_call | pass | 200 | 3936 ms |
| auto | responses | tool_result | pass | 200 | 7873 ms |
| auto | responses | strict_schema_conflict | fail | 200 | 4816 ms |
| auto | responses | image | pass | 200 | 2621 ms |
| auto | responses | inline_text_file | pass | 200 | 4341 ms |
| auto | anthropic | text | pass | 200 | 3276 ms |
| auto | anthropic | stream | pass | 200 | 3683 ms |
| auto | anthropic | reasoning_observed | pass |  |  ms |
| auto | anthropic | usage_observed | pass |  |  ms |
| auto | anthropic | history | pass | 200 | 4531 ms |
| auto | anthropic | tool_call | pass | 200 | 3867 ms |
| auto | anthropic | tool_result | pass | 200 | 2995 ms |
| auto | anthropic | strict_schema_conflict | skipped |  |  ms |
| auto | anthropic | image | pass | 200 | 3264 ms |
| auto | anthropic | inline_text_file | pass | 200 | 5754 ms |
| hy4-preview | responses | text | pass | 200 | 3826 ms |
| hy4-preview | responses | stream | pass | 200 | 3641 ms |
| hy4-preview | responses | reasoning_observed | pass |  |  ms |
| hy4-preview | responses | usage_observed | pass |  |  ms |
| hy4-preview | responses | history | pass | 200 | 4489 ms |
| hy4-preview | responses | tool_call | pass | 200 | 5104 ms |
| hy4-preview | responses | tool_result | pass | 200 | 5348 ms |
| hy4-preview | responses | strict_schema_conflict | fail | 200 | 12689 ms |
| hy4-preview | responses | image | pass | 200 | 1780 ms |
| hy4-preview | responses | inline_text_file | pass | 200 | 7723 ms |
| hy4-preview | anthropic | text | pass | 200 | 4146 ms |
| hy4-preview | anthropic | stream | pass | 200 | 5493 ms |
| hy4-preview | anthropic | reasoning_observed | pass |  |  ms |
| hy4-preview | anthropic | usage_observed | pass |  |  ms |
| hy4-preview | anthropic | history | pass | 200 | 3150 ms |
| hy4-preview | anthropic | tool_call | pass | 200 | 3448 ms |
| hy4-preview | anthropic | tool_result | pass | 200 | 2996 ms |
| hy4-preview | anthropic | strict_schema_conflict | skipped |  |  ms |
| hy4-preview | anthropic | image | pass | 200 | 1909 ms |
| hy4-preview | anthropic | inline_text_file | pass | 200 | 6879 ms |
| hy3 | responses | text | pass | 200 | 1825 ms |
| hy3 | responses | stream | pass | 200 | 2408 ms |
| hy3 | responses | reasoning_observed | pass |  |  ms |
| hy3 | responses | usage_observed | pass |  |  ms |
| hy3 | responses | history | pass | 200 | 2376 ms |
| hy3 | responses | tool_call | pass | 200 | 2177 ms |
| hy3 | responses | tool_result | pass | 200 | 2045 ms |
| hy3 | responses | strict_schema_conflict | fail | 200 | 3879 ms |
| hy3 | responses | image | pass | 200 | 3059 ms |
| hy3 | responses | inline_text_file | pass | 200 | 2820 ms |
| hy3 | anthropic | text | pass | 200 | 1711 ms |
| hy3 | anthropic | stream | pass | 200 | 2022 ms |
| hy3 | anthropic | reasoning_observed | pass |  |  ms |
| hy3 | anthropic | usage_observed | pass |  |  ms |
| hy3 | anthropic | history | pass | 200 | 1886 ms |
| hy3 | anthropic | tool_call | pass | 200 | 2415 ms |
| hy3 | anthropic | tool_result | pass | 200 | 1730 ms |
| hy3 | anthropic | strict_schema_conflict | skipped |  |  ms |
| hy3 | anthropic | image | pass | 200 | 2906 ms |
| hy3 | anthropic | inline_text_file | pass | 200 | 2068 ms |
| space-bunny | responses | text | pass | 200 | 4028 ms |
| space-bunny | responses | stream | pass | 200 | 1728 ms |
| space-bunny | responses | reasoning_observed | not_observed |  |  ms |
| space-bunny | responses | usage_observed | pass |  |  ms |
| space-bunny | responses | history | pass | 200 | 1980 ms |
| space-bunny | responses | tool_call | pass | 200 | 2012 ms |
| space-bunny | responses | tool_result | fail | 200 | 1969 ms |
| space-bunny | responses | strict_schema_conflict | fail | 200 | 3760 ms |
| space-bunny | responses | image | pass | 200 | 2043 ms |
| space-bunny | responses | inline_text_file | pass | 200 | 2469 ms |
| space-bunny | anthropic | text | pass | 200 | 3883 ms |
| space-bunny | anthropic | stream | pass | 200 | 1998 ms |
| space-bunny | anthropic | reasoning_observed | not_observed |  |  ms |
| space-bunny | anthropic | usage_observed | pass |  |  ms |
| space-bunny | anthropic | history | pass | 200 | 2449 ms |
| space-bunny | anthropic | tool_call | pass | 200 | 1878 ms |
| space-bunny | anthropic | tool_result | pass | 200 | 1973 ms |
| space-bunny | anthropic | strict_schema_conflict | skipped |  |  ms |
| space-bunny | anthropic | image | pass | 200 | 2687 ms |
| space-bunny | anthropic | inline_text_file | pass | 200 | 2353 ms |
| glm-5.3-flash | responses | text | pass | 200 | 1892 ms |
| glm-5.3-flash | responses | stream | pass | 200 | 1666 ms |
| glm-5.3-flash | responses | reasoning_observed | not_observed |  |  ms |
| glm-5.3-flash | responses | usage_observed | pass |  |  ms |
| glm-5.3-flash | responses | history | pass | 200 | 2122 ms |
| glm-5.3-flash | responses | tool_call | pass | 200 | 5564 ms |
| glm-5.3-flash | responses | tool_result | pass | 200 | 1925 ms |
| glm-5.3-flash | responses | strict_schema_conflict | fail | 200 | 1747 ms |
| glm-5.3-flash | responses | image | pass | 200 | 1534 ms |
| glm-5.3-flash | responses | inline_text_file | pass | 200 | 1999 ms |
| glm-5.3-flash | anthropic | text | pass | 200 | 1909 ms |
| glm-5.3-flash | anthropic | stream | pass | 200 | 1585 ms |
| glm-5.3-flash | anthropic | reasoning_observed | pass |  |  ms |
| glm-5.3-flash | anthropic | usage_observed | pass |  |  ms |
| glm-5.3-flash | anthropic | history | pass | 200 | 1871 ms |
| glm-5.3-flash | anthropic | tool_call | pass | 200 | 5366 ms |
| glm-5.3-flash | anthropic | tool_result | pass | 200 | 5401 ms |
| glm-5.3-flash | anthropic | strict_schema_conflict | skipped |  |  ms |
| glm-5.3-flash | anthropic | image | pass | 200 | 1900 ms |
| glm-5.3-flash | anthropic | inline_text_file | fail | 200 | 6004 ms |
| glm-5.2 | responses | text | pass | 200 | 1746 ms |
| glm-5.2 | responses | stream | pass | 200 | 1565 ms |
| glm-5.2 | responses | reasoning_observed | not_observed |  |  ms |
| glm-5.2 | responses | usage_observed | pass |  |  ms |
| glm-5.2 | responses | history | pass | 200 | 1856 ms |
| glm-5.2 | responses | tool_call | pass | 200 | 2060 ms |
| glm-5.2 | responses | tool_result | pass | 200 | 1953 ms |
| glm-5.2 | responses | strict_schema_conflict | fail | 200 | 2092 ms |
| glm-5.2 | responses | image | pass | 200 | 1792 ms |
| glm-5.2 | responses | inline_text_file | pass | 200 | 1525 ms |
| glm-5.2 | anthropic | text | pass | 200 | 1430 ms |
| glm-5.2 | anthropic | stream | pass | 200 | 1829 ms |
| glm-5.2 | anthropic | reasoning_observed | not_observed |  |  ms |
| glm-5.2 | anthropic | usage_observed | pass |  |  ms |
| glm-5.2 | anthropic | history | pass | 200 | 1803 ms |
| glm-5.2 | anthropic | tool_call | pass | 200 | 2060 ms |
| glm-5.2 | anthropic | tool_result | pass | 200 | 1853 ms |
| glm-5.2 | anthropic | strict_schema_conflict | skipped |  |  ms |
| glm-5.2 | anthropic | image | pass | 200 | 1530 ms |
| glm-5.2 | anthropic | inline_text_file | pass | 200 | 1783 ms |
| glm-5.1 | responses | text | pass | 200 | 1550 ms |
| glm-5.1 | responses | stream | pass | 200 | 1550 ms |
| glm-5.1 | responses | reasoning_observed | not_observed |  |  ms |
| glm-5.1 | responses | usage_observed | pass |  |  ms |
| glm-5.1 | responses | history | pass | 200 | 2816 ms |
| glm-5.1 | responses | tool_call | pass | 200 | 1524 ms |
| glm-5.1 | responses | tool_result | pass | 200 | 2792 ms |
| glm-5.1 | responses | strict_schema_conflict | fail | 200 | 1759 ms |
| glm-5.1 | responses | image | pass | 200 | 6634 ms |
| glm-5.1 | responses | inline_text_file | pass | 200 | 2993 ms |
| glm-5.1 | anthropic | text | pass | 200 | 2766 ms |
| glm-5.1 | anthropic | stream | pass | 200 | 1837 ms |
| glm-5.1 | anthropic | reasoning_observed | not_observed |  |  ms |
| glm-5.1 | anthropic | usage_observed | pass |  |  ms |
| glm-5.1 | anthropic | history | pass | 200 | 2916 ms |
| glm-5.1 | anthropic | tool_call | pass | 200 | 3104 ms |
| glm-5.1 | anthropic | tool_result | pass | 200 | 2707 ms |
| glm-5.1 | anthropic | strict_schema_conflict | skipped |  |  ms |
| glm-5.1 | anthropic | image | pass | 200 | 1781 ms |
| glm-5.1 | anthropic | inline_text_file | pass | 200 | 2725 ms |
| glm-5v-turbo | responses | text | pass | 200 | 1901 ms |
| glm-5v-turbo | responses | stream | pass | 200 | 1252 ms |
| glm-5v-turbo | responses | reasoning_observed | not_observed |  |  ms |
| glm-5v-turbo | responses | usage_observed | pass |  |  ms |
| glm-5v-turbo | responses | history | pass | 200 | 1600 ms |
| glm-5v-turbo | responses | tool_call | pass | 200 | 2540 ms |
| glm-5v-turbo | responses | tool_result | pass | 200 | 1493 ms |
| glm-5v-turbo | responses | strict_schema_conflict | fail | 200 | 3077 ms |
| glm-5v-turbo | responses | image | pass | 200 | 1855 ms |
| glm-5v-turbo | responses | inline_text_file | pass | 200 | 16322 ms |
| glm-5v-turbo | anthropic | text | pass | 200 | 1688 ms |
| glm-5v-turbo | anthropic | stream | pass | 200 | 1645 ms |
| glm-5v-turbo | anthropic | reasoning_observed | not_observed |  |  ms |
| glm-5v-turbo | anthropic | usage_observed | pass |  |  ms |
| glm-5v-turbo | anthropic | history | pass | 200 | 1324 ms |
| glm-5v-turbo | anthropic | tool_call | pass | 200 | 3053 ms |
| glm-5v-turbo | anthropic | tool_result | pass | 200 | 2964 ms |
| glm-5v-turbo | anthropic | strict_schema_conflict | skipped |  |  ms |
| glm-5v-turbo | anthropic | image | pass | 200 | 1683 ms |
| glm-5v-turbo | anthropic | inline_text_file | pass | 200 | 1509 ms |
| kimi-k3-1 | responses | text | pass | 200 | 4164 ms |
| kimi-k3-1 | responses | stream | pass | 200 | 4492 ms |
| kimi-k3-1 | responses | reasoning_observed | pass |  |  ms |
| kimi-k3-1 | responses | usage_observed | pass |  |  ms |
| kimi-k3-1 | responses | history | pass | 200 | 4395 ms |
| kimi-k3-1 | responses | tool_call | pass | 200 | 7963 ms |
| kimi-k3-1 | responses | tool_result | pass | 200 | 7472 ms |
| kimi-k3-1 | responses | strict_schema_conflict | fail | 200 | 6030 ms |
| kimi-k3-1 | responses | image | pass | 200 | 5916 ms |
| kimi-k3-1 | responses | inline_text_file | pass | 200 | 5404 ms |
| kimi-k3-1 | anthropic | text | pass | 200 | 5715 ms |
| kimi-k3-1 | anthropic | stream | pass | 200 | 5295 ms |
| kimi-k3-1 | anthropic | reasoning_observed | pass |  |  ms |
| kimi-k3-1 | anthropic | usage_observed | pass |  |  ms |
| kimi-k3-1 | anthropic | history | pass | 200 | 4847 ms |
| kimi-k3-1 | anthropic | tool_call | pass | 200 | 6382 ms |
| kimi-k3-1 | anthropic | tool_result | pass | 200 | 8125 ms |
| kimi-k3-1 | anthropic | strict_schema_conflict | skipped |  |  ms |
| kimi-k3-1 | anthropic | image | pass | 200 | 8683 ms |
| kimi-k3-1 | anthropic | inline_text_file | pass | 200 | 5762 ms |
| kimi-k2.7 | responses | text | pass | 200 | 1872 ms |
| kimi-k2.7 | responses | stream | pass | 200 | 2216 ms |
| kimi-k2.7 | responses | reasoning_observed | pass |  |  ms |
| kimi-k2.7 | responses | usage_observed | pass |  |  ms |
| kimi-k2.7 | responses | history | pass | 200 | 1851 ms |
| kimi-k2.7 | responses | tool_call | pass | 200 | 2685 ms |
| kimi-k2.7 | responses | tool_result | pass | 200 | 2246 ms |
| kimi-k2.7 | responses | strict_schema_conflict | fail | 200 | 3197 ms |
| kimi-k2.7 | responses | image | pass | 200 | 2321 ms |
| kimi-k2.7 | responses | inline_text_file | pass | 200 | 3113 ms |
| kimi-k2.7 | anthropic | text | pass | 200 | 3034 ms |
| kimi-k2.7 | anthropic | stream | pass | 200 | 3590 ms |
| kimi-k2.7 | anthropic | reasoning_observed | pass |  |  ms |
| kimi-k2.7 | anthropic | usage_observed | pass |  |  ms |
| kimi-k2.7 | anthropic | history | pass | 200 | 2076 ms |
| kimi-k2.7 | anthropic | tool_call | pass | 200 | 2245 ms |
| kimi-k2.7 | anthropic | tool_result | pass | 200 | 2593 ms |
| kimi-k2.7 | anthropic | strict_schema_conflict | skipped |  |  ms |
| kimi-k2.7 | anthropic | image | pass | 200 | 2189 ms |
| kimi-k2.7 | anthropic | inline_text_file | pass | 200 | 3516 ms |
| kimi-k2.6 | responses | text | pass | 200 | 4532 ms |
| kimi-k2.6 | responses | stream | pass | 200 | 1878 ms |
| kimi-k2.6 | responses | reasoning_observed | not_observed |  |  ms |
| kimi-k2.6 | responses | usage_observed | pass |  |  ms |
| kimi-k2.6 | responses | history | pass | 200 | 1993 ms |
| kimi-k2.6 | responses | tool_call | pass | 200 | 2095 ms |
| kimi-k2.6 | responses | tool_result | pass | 200 | 1488 ms |
| kimi-k2.6 | responses | strict_schema_conflict | fail | 200 | 1784 ms |
| kimi-k2.6 | responses | image | pass | 200 | 2203 ms |
| kimi-k2.6 | responses | inline_text_file | pass | 200 | 1776 ms |
| kimi-k2.6 | anthropic | text | pass | 200 | 1975 ms |
| kimi-k2.6 | anthropic | stream | pass | 200 | 1759 ms |
| kimi-k2.6 | anthropic | reasoning_observed | not_observed |  |  ms |
| kimi-k2.6 | anthropic | usage_observed | pass |  |  ms |
| kimi-k2.6 | anthropic | history | pass | 200 | 1559 ms |
| kimi-k2.6 | anthropic | tool_call | pass | 200 | 2395 ms |
| kimi-k2.6 | anthropic | tool_result | pass | 200 | 2043 ms |
| kimi-k2.6 | anthropic | strict_schema_conflict | skipped |  |  ms |
| kimi-k2.6 | anthropic | image | pass | 200 | 1845 ms |
| kimi-k2.6 | anthropic | inline_text_file | pass | 200 | 1736 ms |
| minimax-m3 | responses | text | pass | 200 | 1829 ms |
| minimax-m3 | responses | stream | pass | 200 | 1825 ms |
| minimax-m3 | responses | reasoning_observed | not_observed |  |  ms |
| minimax-m3 | responses | usage_observed | pass |  |  ms |
| minimax-m3 | responses | history | pass | 200 | 2968 ms |
| minimax-m3 | responses | tool_call | pass | 200 | 2725 ms |
| minimax-m3 | responses | tool_result | pass | 200 | 2106 ms |
| minimax-m3 | responses | strict_schema_conflict | fail | 200 | 2328 ms |
| minimax-m3 | responses | image | pass | 200 | 1991 ms |
| minimax-m3 | responses | inline_text_file | fail | 200 | 1431 ms |
| minimax-m3 | anthropic | text | pass | 200 | 1654 ms |
| minimax-m3 | anthropic | stream | pass | 200 | 1535 ms |
| minimax-m3 | anthropic | reasoning_observed | not_observed |  |  ms |
| minimax-m3 | anthropic | usage_observed | pass |  |  ms |
| minimax-m3 | anthropic | history | pass | 200 | 1649 ms |
| minimax-m3 | anthropic | tool_call | pass | 200 | 1976 ms |
| minimax-m3 | anthropic | tool_result | pass | 200 | 2115 ms |
| minimax-m3 | anthropic | strict_schema_conflict | skipped |  |  ms |
| minimax-m3 | anthropic | image | pass | 200 | 1882 ms |
| minimax-m3 | anthropic | inline_text_file | fail | 200 | 2020 ms |
| deepseek-v4-pro | responses | text | pass | 200 | 2310 ms |
| deepseek-v4-pro | responses | stream | pass | 200 | 2077 ms |
