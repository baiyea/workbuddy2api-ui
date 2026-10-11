# 国际区模型能力验证（2026-10-11）

## 国际区：新增账号后的完整探测

新增国际账号后，使用同一脚本对21个global入口全部发起探测：主批次299次请求、定点复测21次、原Chat对照2次，共322次网关HTTP请求。20个入口能完成文本对话；`deep-model`上游服务缺失。请求数不等于上游重试次数或计费次数。

**读表：** 双值顺序为Responses / Messages；单值代表两者相同。✓表示该用例通过；⚠表示首轮/复测间有成功也有HTTP错误；502表示该项两次均为HTTP502；△表示读到正确文本但输出格式不稳定；✗表示功能断言未通过；—表示未执行。工具调用失败时跳过回填，不把跳过标为不支持。

| 国际区模型 ID | 对话/流式/多轮 | 工具调用 | 工具回填 | 图片 | 文本附件 | 返回推理 | strict Schema |
|---|---|---|---|---|---|---|---|
| `global:default-model` | ✓ | ✓ | ✓ | ✓ | △ | ✓ | 未生效 |
| `global:fast-model` | ✓ | ✓ | ✓ | ✓ | ✓ | 未见 | 未生效 |
| `global:balanced-model` | ✓ | ✓ | ✓ | ✓ | ✓ | 未见 | 未生效 |
| `global:primary-model` | ✓ | ✓ | ✓ | ✓ | ✓ | 未见/✓ | 未生效 |
| `global:hy4-preview` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `global:gpt-5.6-sol` | ✓ | ✓ | ✓ | ✓ | ✓ | 未见 | 未生效 |
| `global:gpt-5.6-terra` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `global:deep-model` | 不可用 | — | — | — | — | — | — |
| `global:deepseek-v4.1-flash` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `global:gpt-6-astra` | ⚠ | ⚠ | ✓ | ⚠/✓ | 502/✓ | 未见/✓ | 未生效 |
| `global:hy4-preview-f` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `global:hy3` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `global:glm-5.2` | ✓ | ✓ | ✓ | ✗ | ✓ | 未见 | 未生效 |
| `global:gpt-5.6-luna` | ✓ | ✓ | ✓ | ✓ | ✓ | 未见 | 未生效 |
| `global:gpt-5.5` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `global:gpt-5.4` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `global:gpt-5.3-codex` | ✓ | ✓/⚠ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `global:gemini-3.5-flash` | ✓ | 502 | — | ✓ | ✓ | 未见 | 未生效 |
| `global:glm-5.3` | ✓ | ✓ | ✓ | ✗ | ✓ | ✓ | 未生效 |
| `global:kimi-k3` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `global:kimi-k2.6` | ✓ | ✓ | ✓ | ✓ | ✓ | 未见 | 未生效 |

- **deep-model**：三个文本请求（含原Chat对照）均503。脱敏日志记录上游400/code11102：`model [deep-model] service info not found`，因此其余能力未测。
- **gpt-6-astra**：两协议间歇502。Responses流式、工具、图片复测成功，但文本附件两次502；Messages首轮流式/工具成功，复测出现502。原Chat流式对照返回437并正常结束。表中保留异常，不能宣称稳定兼容，也不能据此判定原生模型没有能力。
- **gemini-3.5-flash**：两转换协议工具调用连续两轮502，回填未执行；原Chat非流式对照成功生成`probe_echo`，参数值正确。已经观察到转换路径差异，尚未定位具体原因，本轮未改转换代码。
- **gpt-5.3-codex**：Messages工具首轮503，复测调用及回填通过；本轮日志记录一次上游524。保留偶发失败标记。
- **glm-5.2 / glm-5.3**：图片两协议两轮均失败，多数响应明确提示当前模型不支持图像输入；一次答错颜色。国内同名入口之前通过，不能把两区结果混用。
- **default-model**：附件首轮两协议多输出`test_value=`前缀，复测两者通过，保留格式偏差。
- **Schema**：20个可对话入口均未遵守Responses strict Schema，均正常完成、HTTP200。Messages输出Schema依网关边界跳过；deep-model未执行Schema测试。
- 返回推理列合并首轮及复测所有用例；“未见”不意味着不具备推理能力，未测试显式effort/budget控制。可对话的20个入口均在两种协议中观察到用量。

### 国际区证据与脚本

国际区没有需要另建测试实现的参数差异，继续复用`scripts/probe_model_capabilities.py`，按完整`global:`ID测试。示例（先设置WB2A_API_KEY）：

```powershell
python scripts/probe_model_capabilities.py --base-url http://127.0.0.1:7863/v1 --model "GPT=global:gpt-5.6-sol" --output docs/superpowers/verification/global-gpt-results.json
```

本轮按模型单独保存报告，全部21份都有completed_at，未再次发生Windows报告替换中断。模型详情路径保存在机器可读表的report字段；[Chat对照](global-models-20261011/chat-controls.json)、[Astra复测](global-models-20261011/confirm-astra.json)、[工具复测](global-models-20261011/confirm-tools.json)、[图片复测](global-models-20261011/confirm-images.json)、[附件复测](global-models-20261011/confirm-file.json)、[脱敏上游错误](global-models-20261011/upstream-warnings.txt)分别保留。

仅把国际账号副本放入隔离测试池，避免国内账号混入；定时任务关闭。临时容器和网络已清理，两个原始账号文件SHA256一致；没有发布、推送、部署或重启用户实际服务。本轮修改仅为README用法及验证报告。路由别名及各模型ID是按网关目录测试的入口，不独立证明底层模型身份或永久可用性。
