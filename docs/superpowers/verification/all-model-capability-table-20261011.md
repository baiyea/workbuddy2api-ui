# 全量模型能力表（2026-10-11）

已覆盖目录中的37个入口：国内16个、国际21个。国内结果来自同日两轮验证；国际区在用户新增账号后完成322次网关请求。国际deep-model服务缺失，其余20个入口能完成对话，但部分功能存在下表列出的错误或格式偏差。国内扩展轮195次有效矩阵请求、2次中断前重复和8次定点复测的历史证据保留。

**读表：** ✓ 表示本轮用例通过；双值按 **Responses / Messages** 排列，单值代表两者相同。△表示读到正确标记但格式/回答不稳定；“未见”表示所有该协议用例中没有返回推理文本，不等于不支持推理。Schema列只指Responses；Messages输出Schema由网关明确拒绝，全部跳过。

| 模型 ID | 对话/流式/多轮 | 工具调用 | 工具回填 | 图片 | 文本附件 | 返回推理 | strict Schema |
|---|---|---|---|---|---|---|---|
| `cn:auto` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `cn:hy4-preview` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `cn:hy3` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `cn:space-bunny` | ✓ | ✓ | ✗/✓ | ✓ | ✓ | ✓ | 未生效 |
| `cn:deepseek-v4.1-flash` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `cn:glm-5.3` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `cn:glm-5.3-flash` | ✓ | ✓ | ✓ | ✓ | △ | ✓ | 未生效 |
| `cn:glm-5.2` | ✓ | ✓ | ✓ | ✓ | ✓ | 未见 | 未生效 |
| `cn:glm-5.1` | ✓ | ✓ | ✓ | ✓ | ✓ | 未见/✓ | 未生效 |
| `cn:glm-5v-turbo` | ✓ | ✓ | ✓ | ✓ | ✓ | 未见 | 未生效 |
| `cn:kimi-k3-1` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `cn:kimi-k2.8-preview` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `cn:kimi-k2.7` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |
| `cn:kimi-k2.6` | ✓ | ✓ | ✓ | ✓ | ✓ | 未见 | 未生效 |
| `cn:minimax-m3` | ✓ | ✓ | ✓ | ✓ | △ | 未见 | 未生效 |
| `cn:deepseek-v4-pro` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | 未生效 |

国内区全部32个已测“模型×协议”组合均观察到返回用量。`cn:auto` 是自动选模入口，结果不能绑定到固定底层模型。DeepSeek V4.1 Flash、GLM5.3、Kimi K2.8 Preview使用同日上一轮结果，其余为本轮新增测试。

## 异常与复测

- `space-bunny`：Responses工具调用两次均成功，回填两次均答错（首次输出类似工具ID，第二次输出“tool returned code 0”，均非实际传入的随机结果）；Messages两次回填均通过。标记的是当前调用链路的用例失败，不推断模型原生接口完全不支持。
- `glm-5.3-flash`：Responses附件首轮通过、复测多输出说明及代码块；Messages两次均未满足“只输出值”的格式，均包含正确标记，复测还否认附件形式。能读取转换后的文本，不保证严格格式或对附件形式的理解。
- `minimax-m3`：两协议首轮都多输出`test_value=`前缀，复测两者均只输出正确值。保留格式不稳定标记，没有用复测覆盖首轮。
- 全部16个国内入口的Responses strict Schema冲突用例未通过：均HTTP200、正常结束，但未遵守指定JSON结构。当前链路不能保证严格Schema；不是HTTP或网关解析失败。
- `glm-5.2`、`glm-5v-turbo`、`kimi-k2.6`、`minimax-m3`本轮未返回推理文本；`glm-5.1`仅Messages用例观察到。没有显式探测effort或budget档位，因此不能判定原生模型没有推理能力。

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

## 国内区证据与运行边界

- 使用现有通用脚本，测试实际console→core→WorkBuddy；脚本串行、不重试、不执行真实工具。配置关闭六类定时任务，账号使用隔离副本，随机本机端口。
- 核心源码与镜像身份、记录计数、所有源报告SHA256见[机器可读表](all-model-capability-table-20261011.json)。本轮未修改业务代码或测试脚本。
- [本轮主批次JSON](all-other-model-capabilities-20261011.json) / [逐项表](all-other-model-capabilities-20261011.md)：前12个模型完整；最后模型开始后Windows文件替换遇到WinError5，留下检查点，未丢弃此前结果。
- [DeepSeek V4 Pro续跑JSON](all-model-deepseek-v4-pro-20261011.json) / [逐项表](all-model-deepseek-v4-pro-20261011.md)：完整15项请求；汇总采用此文件替代主批次该模型的2个重复请求。
- [工具回填复测JSON](all-model-tool-confirmation-20261011.json) / [附件复测JSON](all-model-file-confirmation-20261011.json)，首轮与复测分别保留。
- [上一轮3模型汇总](real-model-capabilities-20261011-summary.md)。能力表中的推理列根据全部用例返回内容合并，原脚本的reasoning_observed记录仅来自算术流式用例，二者口径不同。
- 主矩阵全部实际请求均HTTP200；脚本退出1是能力断言失败的预期信号。主批次另有一次Windows文件占用导致的报告保存中断，已用独立报告续跑；尚未修改脚本为此添加文件重试。
- 临时容器及网络已清理，原账号文件SHA256一致；没有发布、推送、部署或重启用户实际服务。真实日志和凭据副本保留在忽略的.build；公开报告不含密钥。

这是一组有限用例，不能证明长上下文、复杂图片、PDF、Files存储、状态化Responses、所有工具选择策略、精确推理预算或任意客户端完全兼容。每个功能首轮仅一次，异常定点复测一次；Schema不做生成后修复。
