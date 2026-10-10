# Global Constraints

- 不修改 `upstream/` 或 `upstream-convert/`；既有快照文件和第三方库修正通过 `patches/`。
- RelayKit 固定本地 New API commit `0f38a14a5e8e322ea741478b0e49878be3130e1d`；保留原 module 路径、许可证与署名。
- 保持两个服务、同一账号池、无 HTTP 回环；只在 bridge 分支包装。
- 未知用量为 null、真实零保留、采用最后明确累计值；不伪造成功终态。
- 请求限制使用现有配置；JSON 响应和 SSE 累计上限均为 32 MiB，SSE 单帧 8 MiB。
- 保留认证、owner 上下文、取消；不调用真实上游、不重启实际服务。
- 不新增 Files API、response 存储、后台工具执行、JSON Schema 修复或管理业务。
- 所有文档/中间记录位于 `docs/superpowers`；保留原有 `.gitignore`、探测报告和 `upstream-convert/`。
- 不自动发布、推送、部署；确认实施只授权本地开发和隔离验证。


## Task 5: SDK、隔离验收、说明与最终审查

**Files:** `scripts/check_anthropic_sdk.py`、新增 `scripts/check_responses_sdk.py`、`scripts/check.sh`、`scripts/acceptance.sh`、`deploy/mock_upstream.py`；`README.md`、`AGENTS.md`、`console/web/app.js` 与已有 API 提示/署名位置、`docs/superpowers/verification/`。

**Interfaces:** SDK 仅用于临时测试环境，不进入生产依赖；复用隔离 console → core → mock 链路。Responses SDK 的验收版本在测试脚本依赖说明中锁定并记录实际版本。

- [ ] **SDK 先补失败断言。** Anthropic 保留 null/晚到 usage、raw events、text_stream、最终聚合，增加工具。Responses 覆盖 create、stream 事件/最终对象、自定义工具 ID 和参数。传输测试不代替 SDK 验收。
- [ ] **扩展隔离集成。** 实际 console/core 连 mock 检查两协议，保留 OpenAI 流式及管理 owner/CSRF/取消断言；验证 JSON Schema 到达 mock 上游，不验证或修复生成结果。将 protocol/responses 加入 check.sh 的关键包竞态检查；不使用真实账号。
- [ ] **更新说明。** README 面向使用者写端点/curl/能力范围；AGENTS 记录目录与验证方式；页面修订“仅文本”提示，保留模型依赖和 beta 限制及第三方署名。无新增交互业务。
- [ ] **执行检查并记录退出码。** 命令如下；SDK 的临时 Python 路径和锁定版本写入记录，未执行的单独标记。

```text
python -m unittest discover -s scripts -p test_*.py -v
python -m unittest discover -s deploy -p test_*.py -v
python -m unittest discover -s desktop/scripts -p test_*.py -v
bash scripts/check.sh
docker compose --env-file /dev/null -f docker-compose.yml config --quiet
bash scripts/acceptance.sh
git diff --check
```

带 `/dev/null` 的命令在可用 Bash/Linux 环境执行。SDK 环境沿用既有隔离方式，Anthropic 使用现有 WB2A_SDK_PYTHON；Responses 解释器以相同机制传入。不为测试改变宿主全局环境或重启实际服务。

- [ ] **独立审查。** 审查整个 diff 与 Review Focus，确认无新增服务/会话绕过/数据或密钥混入；关键问题先复现失败、最小修正、复验。
- [ ] **交付。** 说明修改、实际测试、未做项目及能力限制；保留用户原改动。Docker 不可用或 SDK 未执行即对应验收未完成，不声称已发布/部署。


Read root AGENTS.md and the referenced design spec. Do not create worktrees or branches. Do not spawn subagents. Keep reports/logs under docs/superpowers.
