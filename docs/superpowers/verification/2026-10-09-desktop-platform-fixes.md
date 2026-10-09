# 桌面端平台审查问题修复

日期：2026-10-09。分支：`codex/native-desktop`。本轮修复独立代码审查发现的 Windows 原生构建阻断和 macOS 最低版本不一致。

## 修改

- Windows 物化冻结快照时，从 Git 索引获取文件模式，仍逐个读取并校验工作区文件的实际字节。未跟踪文件、类型改变、内容改变或 Git 模式改变均不能绕过摘要校验。Unix 沿用实际文件系统执行位。没有修改 `upstream/` 或 `upstream.lock`。
- `.gitattributes` 禁止 Git 对 `upstream/` 与 `patches/` 自动转换换行，保持锁定快照及补丁字节。Windows 构建需要 Git 检出；已有被换行转换的目录不会被脚本自动重写。
- macOS 最低版本统一为 12.0。资源发布前扫描所有 Mach-O 文件，检查 core、console、Python 和共享库的最低部署版本；不兼容或缺少部署版本时失败，已有资源保持不变。编译器/链接器工具版本不作为系统版本。
- Windows 回归夹具使用显式 Git 文件模式生成锁，不依赖宿主 `chmod` 能否设置可执行位。

## 实际验证

- 先复现 Windows 摘要失败；新增测试使用真实临时 Git 克隆、`core.autocrlf=true` 和移除执行位模拟 Windows 检出。修复后物化及补丁应用通过，内容与索引模式篡改仍被拒绝。
- 脚本测试 44 项、部署工具单元测试 13 项、桌面准备测试 9 项通过。
- `bash scripts/check.sh` 通过，包括物化 core 的 Go 测试/vet/关键包竞态测试、console 竞态测试、33 项 Node 页面测试与 15 项 Python 任务测试。
- 两种 macOS 架构通过 `npm run build` 重新构建。最终 `.app` 的 `LSMinimumSystemVersion` 均为 12.0，整个应用包的 Mach-O 文件（包括外壳、Go 服务与 Python 库）最低版本均不高于 12.0。
- 两个最终 DMG 均通过 `hdiutil verify` 校验；默认资源目录已恢复为 arm64。
- 新 arm64 包内资源的隔离集成测试通过：空账号启动、管理登录、六类任务目录、停止及重启后密钥保留。未请求真实上游。
- Compose 配置校验通过。本轮没有重跑 Docker 容器验收，先前结果见 `desktop/docs/verification.md`。
- 本轮代码范围的 `git diff --check` 通过。全工作区检查提示用户新增的 `AGENTS.md:14` 有尾部空格；该编辑保留原样，本轮未修改 AGENTS。
- 独立复审确认原先两项问题及测试夹具可移植性问题均已修正，无剩余审查问题。

## 产物与边界

- `desktop/target/aarch64-apple-darwin/release/bundle/dmg/wb2api-desktop-macos-arm64-0.1.0.dmg`
- `desktop/target/x86_64-apple-darwin/release/bundle/dmg/wb2api-desktop-macos-x64-0.1.0.dmg`

Windows 检出验证是 macOS 上的隔离模拟，不代表 Windows 真机安装和运行验收。安装包仍未正式签名公证。本轮未提交、推送、发布或部署；保留用户对 AGENTS 的编辑以及此前桌面端工作区改动。
