# 原生桌面运行器 Implementation Plan

**Goal:** 在 desktop/ 交付可构建的 Tauri 托盘应用和自包含运行资源准备流程。
**Architecture:** Rust 托盘管理原生 Go core/console 子进程，Python 随资源分发。core 通过父进程管道取消生命周期；网页复用现有登录。
**Tech Stack:** Tauri 2 / Rust、Go、Python 标准库、原生 JS。
**Spec:** desktop/docs/design.md

## Global Constraints
- 用户端不安装 Docker、Go、Node 或 Python；构建期下载锁定依赖。
- 仅本机监听，不使用真实数据，不修改 upstream/；不发布、推送、重启真实服务。
- 所有客户端源文件和构建工具放 desktop/，共享功能按原仓库目录边界调整。

## Tasks
- [x] Core：先测父管道 EOF 取消及取消后历史持久化；实现 WB2A_DESKTOP=true 时的管道生命周期，Windows 文件持久化适配，通过扩展/新补丁接入；测试原 Docker 路径不变。
- [x] Console：先测试 URL 密钥预填、清参、不自动提交、会话失败不清掉预填值、非本机拒绝预填；接入前端和桌面文案。增加可取消服务退出。
- [x] Desktop：先测 URL 编码、固定监听、资源路径/环境和子进程退出；实现 Rust 单实例托盘、按序启动、就绪检测、打开浏览器、退出回收、失败通知。Windows Job Object / Unix 进程组兜底。
- [x] Packaging：锁定 Python 发行物 URL/SHA256，构建期校验后解包；overlay 后构建原生 core/console，打包 Python/scripts/许可证，Tauri 离线安装配置。恶意路径和摘要失败测试。
- [x] Integration：本机隔离启动、登录、重启数据保留与停止进程；Go/Node/Python/Rust 检查；现有 scripts/check.sh 与隔离 acceptance.sh；明确记录 Windows 原生运行未实测的范围。

## Progress
- 初始工作区干净；分支 `codex/native-desktop`。Rust 构建工具隔离安装在 `.build/desktop-tools/`。
- 已完成源代码、macOS 两种架构开发包和隔离运行检查；Windows 已完成资源准备与平台代码交叉检查，尚未完成 Windows 原生安装/运行验收。
- 实际结果与交付边界见 [verification.md](verification.md)；勾选表示此轮实现和列明的检查完成，不表示未执行的平台验收通过。
