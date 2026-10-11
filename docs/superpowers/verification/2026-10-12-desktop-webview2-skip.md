# Windows 托盘安装包移除 WebView2

## 修改

- `desktop/tauri.conf.json` 将 `webviewInstallMode` 从 `offlineInstaller` 改为 `skip`。
- 应用仍使用原生托盘、原生错误对话框及系统默认浏览器，不创建 WebView。
- 同步桌面使用说明、设计说明和 AGENTS；未修改 core、console、Docker 或 Compose。

## 本地构建与安装（2026-10-11）

测试环境为 Windows 11 x64，已安装 WebView2 135.0.3179.98。

| 项目 | 实际结果 |
| --- | --- |
| 桌面 Python 脚本测试 | 11 项通过 |
| 随包资源准备 | 成功；Python 标准库、证书和任务模块导入通过 |
| Rust `cargo test --all-targets --locked` | 11 项通过，1 项原生集成测试默认忽略 |
| 单独运行被忽略的原生集成测试 | 1 项通过，覆盖空账号启动、重启后密钥保留及停止 |
| `npm --prefix desktop run build` | Windows x64 NSIS 安装包构建成功 |
| 包内容检查 | 不含 WebView2 安装器；保留桌面程序、core、console、Python |
| 本机安装 | `/S /UPDATE` 安装退出码 0；安装文件与打包载荷一致 |
| 数据保留 | 安装前已备份；安装后、启动前逐一核对 9 个已有数据文件，无内容变化 |
| 端口冲突 | 显示预期原生错误对话框；未创建服务子进程 |
| 正常启动 | 已安装目录中的桌面程序、core、console 正常运行；`/`、`/livez` 返回 200 |
| 监听与浏览器 | console 仅监听 `127.0.0.1:7863`；默认 Edge 打开控制台标签 |
| WebView2 子进程 | 未发现由桌面程序或两个服务直接启动的 WebView2 进程 |

本地测试构建临时使用 v0.2.0，与已有安装一致；构建结束后恢复仓库清单版本。Tauri 会给包内主程序写入 NSIS 类型标记，因此安装文件应与安装包解出的主程序比对，而不是与打包前的主程序比对。

- 原 Windows 安装包：252861919 字节，241.15 MiB。
- 新 Windows 安装包：36391473 字节，34.71 MiB，减少 85.61%。
- 新包 SHA-256：`cedb4ced51a8b1fcb08b63c78e7876c0fc4c3e899980a0fd75ffcb5f28f31bd6`。

## 验证边界

- 本机已有 WebView2，不能替代未安装 WebView2 的干净 Windows 验收。
- 浏览器页面交互和安装后托盘点击退出尚未完成；Computer Use 因无法可靠识别浏览器 URL 自动停止，未绕过该限制。
- 本地测试发现已有运行器拒绝 CMD 注入的 `=ExitCode` 隐藏环境变量；直接调用 Cargo 后测试通过。此次没有修改运行器环境变量逻辑。
- 本地存在独立 MSVC Rust 和 rustup 管理的 GNU Rust；构建进程使用独立 MSVC 工具链，未修改系统默认工具链。
- 安装包未进行代码签名；未进行真实账号授权、奖励或完整协议回归验收。

2026-10-12 提交前再次运行桌面脚本测试，11 项通过；重新核对安装包大小及 SHA-256，并通过 `git diff --check`。此前的原生构建与安装结果沿用上述实测记录，没有重复声称为本日重测。
