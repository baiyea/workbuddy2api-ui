# 原生桌面运行器验证记录

基础运行验证日期：2026-10-08；命名调整与重新打包：2026-10-09。开发机：macOS Apple Silicon。分支：`codex/native-desktop`。全部服务检查使用隔离临时数据或空账号，没有读取真实账号或请求真实上游。

## 本次构建

| 目标 | 结果 | 安装包位置（相对仓库） |
| --- | --- | --- |
| macOS arm64 | `.app` 与 DMG 构建成功，DMG 47.67 MiB | `desktop/target/aarch64-apple-darwin/release/bundle/dmg/wb2api-desktop-macos-arm64-0.1.0.dmg` |
| macOS x64 | `.app` 与 DMG 构建成功，DMG 47.78 MiB | `desktop/target/x86_64-apple-darwin/release/bundle/dmg/wb2api-desktop-macos-x64-0.1.0.dmg` |
| Windows x64 | Go 服务与锁定 Python 资源准备成功；Windows 运行器代码交叉检查通过 | `.build/desktop-windows-runtime/`（资源目录，不是安装包） |

macOS 为本地开发构建，未完成 Developer ID 分发签名或公证。Windows 完整 Tauri 安装包仍需 Windows/MSVC 构建。生成物和工具缓存被 Git 忽略；默认 `desktop/resources/runtime/` 最终恢复为 macOS arm64。

## 2026-10-09 命名调整

应用名、显示名称、托盘文案与主可执行文件名统一为 `wb2api-desktop`。`npm run build` 读取资源目标与配置版本，完成构建后自动统一安装包文件名。

- 两种 macOS 目标均经新命令重新构建成功；包内 `CFBundleName`、`CFBundleDisplayName`、`CFBundleExecutable` 均为 `wb2api-desktop`。
- 两个 DMG 均通过 `hdiutil verify` 校验；已清理被替代的旧名称 `.app` 和 DMG 构建产物。
- `cargo fmt --check`、`cargo check --all-targets`、Node 构建脚本语法检查及 `git diff --check` 通过。
- Windows 构建入口已配置为生成 `wb2api-desktop-windows-x64-0.1.0.exe`，本次未在 Windows 上构建或运行。
- 本次只调整名称和打包入口；下列功能验收为 2026-10-08 的记录，没有将它们标为重新执行。

## 2026-10-08 已执行并通过

- 仓库 Python 脚本与部署单元测试、Compose 配置检查、`git diff --check`。
- `bash scripts/check.sh`：物化 core 的 Go 测试、vet、关键包竞态测试，console 竞态测试，33 项 Node 页面测试及 15 项 Python 任务测试。
- `bash scripts/acceptance.sh`：fresh/rebuild/legacy 隔离 Docker mock 验收。Docker 构建需通过宿主代理，本次使用仓库提供的 `WB2A_BUILD_HTTP_PROXY` / `WB2A_BUILD_HTTPS_PROXY` 指向 `http://host.docker.internal:7890`；未更改项目默认代理或宿主环境。未设置 `WB2A_SDK_PYTHON`，本次没有运行额外 Anthropic SDK 验收。
- 桌面资源准备单元测试：8 项通过，覆盖摘要校验、路径越界拒绝、内部符号链接、目标清单、任务文件范围及失败回滚。
- Rust `cargo fmt --check`、`cargo clippy --all-targets -- -D warnings`、`cargo test --all-targets`：7 项通过，1 项资源集成测试默认忽略并另行显式执行。两项通过项为供子进程测试调用的 fixture。
- 显式资源集成测试：使用临时目录和随机本机端口启动 core/console，空账号可启动；POST 管理登录成功，任务目录含全部六类任务；停止、重启后基础密钥保留；停止后子进程退出。已分别针对准备目录和最终 arm64 `.app` 包内资源执行。
- Intel `.app` 包内 Go 服务在此 Apple Silicon 开发机的系统兼容环境下完成相同隔离集成检查；对应 Python 的 SSL、CA、标准库及生产任务模块导入检查通过。此项不能代替 Intel 真机验收。
- 进程树测试覆盖父进程正常退出、父进程不响应管道关闭两种情形，验证直接子进程和后代均被回收。
- Windows Go 相关包交叉编译；Rust 运行器、Job Object 创建代码及测试通过 `x86_64-pc-windows-msvc` 目标 `cargo check`。Rust 检查使用临时精简 crate 引用实际运行器源码，未链接 Tauri 或生成 Windows 安装包。

本地详细输出保存在忽略的 `.build/desktop-check-final.log`、`.build/desktop-acceptance-final.log`、`.build/desktop-bundle-smoke.log`、`.build/desktop-intel-bundle-smoke.log`、`.build/desktop-windows-check.log` 和构建日志中。复现命令见 [桌面说明](../README.md)。

## 仍需对应环境验证

- Windows 原生安装、托盘交互、离线依赖安装、文件替换/历史恢复、进程树回收与无黑色控制台窗口。
- macOS 两种架构的干净机器安装、真实托盘点击与默认浏览器交互；本次包内服务验收不等于 GUI 端到端验收。
- 实际 OAuth/扫码/验证码、上游模型响应及六类任务实际业务结果；测试仅验证同一套实现被打包、可导入和目录接通，不证明授权成功或奖励到账。
- 正式分发签名、公证和发布。

未提交或推送 Git，未发布镜像/安装包，未部署或重启真实服务。开始时工作区干净，本次没有需保留的既有无关改动。
