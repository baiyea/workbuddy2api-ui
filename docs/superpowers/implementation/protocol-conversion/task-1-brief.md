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


## Task 1: 固定第三方模块并接入构建

**Files:** `extensions/third_party/relaykit/` 及来源/摘要清单；`patches/0008-relaykit-module.patch`、`patches/series`、`patches/README.md`、`deploy/core.Dockerfile`、`scripts/check.sh`、`desktop/scripts/prepare.py`、`desktop/scripts/test_prepare.py`。

**Interfaces:** 本地 `require github.com/QuantumNous/new-api/relaykit v0.0.0` 与 `replace ... => ./third_party/relaykit`；模块补丁使 core 达到 Go 1.26 基线。原始库接口保持不变。

- [ ] **核对来源并复制。** 核对源 commit 与工作区状态，复制独立模块的受版本控制文件和 LICENSE/NOTICE，记录每个原始文件 SHA-256；保留用户源目录。已归档草稿不能替代来源核对。
- [ ] **建立依赖补丁。** 用尚不存在的 `.build/protocol-deps-<编号>` 物化目录修改 go.mod/go.sum，再生成补丁；另一新目录重新 prepare，确认本地 replace 生效，不从远端选择其他 RelayKit 版本。
- [ ] **先补打包失败测试。** `test_resources_include_relaykit_notices_and_source_provenance` 调用真实复制函数，断言声明内容字节一致且不混入库源码；运行 `python -m unittest discover -s desktop/scripts -p test_prepare.py -v`，先观察缺少复制行为的失败。
- [ ] **最小实现构建/声明接入。** core 构建镜像升至 Go 1.26 系列，独立 test/vet 嵌套模块；Docker 与桌面资源含 LICENSE/NOTICE/来源清单。console 工具链、Compose 不变。
- [ ] **验证。** 新物化 `.build/protocol-check-1`；运行 `go -C .build/protocol-check-1/third_party/relaykit test ./...`、同目录 `vet ./...`、`go -C .build/protocol-check-1 test ./...` 及前述 Python 测试；预期全部退出 0。
- [ ] **记录与审查。** 保存命令、结果、快照摘要；审查模块边界及打包，确认没有账号、密钥或 New API 宿主服务。


Read root AGENTS.md and the referenced design spec. Do not create worktrees or branches. Do not spawn subagents. Keep reports/logs under docs/superpowers.
