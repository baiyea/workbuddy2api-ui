# wb2api-desktop 桌面运行器

Windows/macOS 托盘应用，在本机启动同一套 core、console 和六类任务。页面由默认浏览器打开，监听地址仅为 `127.0.0.1`。关闭浏览器不会停止服务；通过托盘退出，才停止应用管理的服务和任务。已有 Docker 部署入口保持独立。

## 使用者

安装包包含对应平台的 Go 服务、Python 解释器、标准库和任务脚本，无需自行安装 Docker、Go、Node.js 或 Python，也不会在应用首次启动时下载这些依赖。Windows 安装包配置离线 WebView2 安装资源；该部分在构建时取得。

托盘打开本地控制台，管理密钥仅预填登录框，用户仍需点击登录。端口被其他程序占用时提示错误，不会停止其他程序。账号和状态保存在用户应用数据目录，资源目录不用于保存账号。

首批构建目标：Windows x64、macOS Apple Silicon 和 macOS Intel。这里描述的是源码设计和构建入口，安装包是否可交付仍以各平台实际验收结果为准。

macOS 最低支持 **12.0**。资源准备会检查 core、console、Python 及其共享库的 Mach-O 最低版本；任何资源要求更高系统版本都会中止构建，保留已有资源。

本次开发构建的检查结果与尚未验证的范围见 [验证记录](docs/verification.md)。macOS 使用 `~/Library/Application Support/com.workbuddy2api.desktop/`，Windows 使用 `%LOCALAPPDATA%\com.workbuddy2api.desktop\` 保存数据；其中 `logs/` 用于排查启动问题。

后续平台兼容性修复见 [2026-10-09 审查修复记录](../docs/superpowers/verification/2026-10-09-desktop-platform-fixes.md)。

## 开发与构建

从仓库根目录执行。开发机需要 Git、Python **3.12+**（安全 tar 解压）、Go（版本满足两个 `go.mod`）、Node.js/npm、Rust **1.90+** 和对应平台编译工具。macOS 使用 Xcode Command Line Tools；Windows 使用 MSVC C++ 构建工具。这些是开发机要求，不是最终用户要求。

```bash
# 默认选择当前 Windows/macOS 平台；不启动任何业务服务
python3 desktop/scripts/prepare.py

# 安装锁定的构建工具，再构建本机安装包
npm --prefix desktop ci
cd desktop
npm run build
```

Windows 可将 `python3` 换成 `py -3.12`（或已安装的更新版本）。发布用安装包需另行完成平台签名、macOS 公证和干净机器验收。

Windows 构建需要完整 Git 检出。快照校验从 Git 索引读取 Windows 文件系统缺失的可执行模式，同时校验工作区实际内容；`.gitattributes` 保持快照与补丁的原始字节，不受 `core.autocrlf` 影响。已有目录若已被旧换行配置转换，请在新的干净克隆中构建；准备脚本不会自动改写源码或更新锁值。

指定资源目标时：

```bash
python3 desktop/scripts/prepare.py --target aarch64-apple-darwin
python3 desktop/scripts/prepare.py --target x86_64-apple-darwin
python3 desktop/scripts/prepare.py --target x86_64-pc-windows-msvc
```

一次只准备一个目标，随后 `npm run build` 自动读取资源清单选择相同的 Tauri 目标。准备脚本在资源中写入 `runtime-manifest.json`，包含 `target` 和 `python_version`；构建入口据此核对 Cargo 目标，防止把其他平台的服务误装进安装包。完整 Windows 安装包在 Windows 上构建，完整 macOS 安装包在 macOS 上构建。Go 资源可交叉编译，但交叉准备不会声称目标 Python 已运行验证。

安装后的应用名为 `wb2api-desktop`。构建命令从 `tauri.conf.json` 读取版本号，自动将安装包统一命名为 `wb2api-desktop-{系统}-{指令集}-{版本号}.*`，保存到 `desktop/target/<目标>/release/bundle/` 对应子目录。例如版本 `0.1.0`：

- Windows x64：`nsis/wb2api-desktop-windows-x64-0.1.0.exe`
- macOS Apple Silicon：`dmg/wb2api-desktop-macos-arm64-0.1.0.dmg`
- macOS Intel：`dmg/wb2api-desktop-macos-x64-0.1.0.dmg`

macOS 包内应用为 `wb2api-desktop.app`。直接运行底层 `tauri build` 会使用 Tauri 默认安装包文件名，交付构建请使用 `npm run build`。

## GitHub Releases 自动发布

[Desktop Release 工作流](../.github/workflows/desktop-release.yml) 在推送版本标签后，分别在 Windows 和两种架构的 macOS runner 上测试、准备运行环境并打包。三个目标全部成功后，才发布同一个 GitHub Release；不构建 Docker 镜像，也不部署服务器。

先提交并推送桌面代码、锁文件和工作流，再给要发布的提交打标签。例如：

```bash
git tag v0.1.0
git push origin v0.1.0
```

标签是该次构建版本号的唯一来源。CI 自动同步构建目录中的 Tauri、Cargo、npm 清单及锁文件里的应用版本，不回写仓库，也不更新依赖版本。支持 `vX.Y.Z`，以及 `vX.Y.Z-alpha.N`、`vX.Y.Z-beta.N`、`vX.Y.Z-rc.N`；后面三种自动标为预发布。数字不得带多余前导零，范围为 0–65535。

例如推送 `v0.1.0` 后，Release 附件为：

| 附件 | 平台 |
| --- | --- |
| `wb2api-desktop-windows-x64-0.1.0.exe` | Windows x64 |
| `wb2api-desktop-macos-arm64-0.1.0.dmg` | macOS Apple Silicon |
| `wb2api-desktop-macos-x64-0.1.0.dmg` | macOS Intel |
| `SHA256SUMS.txt` | 三个安装包的 SHA-256 校验值 |

工作流使用仓库自带的 `GITHUB_TOKEN`，只有汇总发布 job 获得 `contents: write` 权限，无需配置个人访问令牌。仓库需允许 GitHub Actions 运行相关官方 Actions。runner 架构依据 [GitHub 官方列表](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)，固定使用 `windows-2022`、`macos-15`（arm64）、`macos-15-intel`。

构建或测试失败时不发布。附件先上传到草稿，上传成功再公开；上传阶段失败留下的草稿可通过重跑失败 job 补齐。已公开的同版本 Release 不覆盖，请使用新标签。CI 会生成发布说明，但不会修改既有公开版本。

当前工作流延续本地构建的签名状态：没有配置 Windows 代码签名或 Apple Developer ID 签名、公证，系统可能拦截或提示无法验证开发者。CI 的原生运行测试不等于安装器、托盘交互及真实账号的端到端验收。

## 构建资源与依赖

构建脚本读取本仓库冻结快照，经 `scripts/overlay.py prepare` 物化到新临时目录，使用 `CGO_ENABLED=0` 和目标 `GOOS/GOARCH` 编译 core、console，不修改 `upstream/`。全部阶段成功后才替换 `desktop/resources/runtime/`；普通替换失败恢复旧目录。若构建进程在替换期间被强制结束，保留的 `runtime.previous/` 需人工检查，不会自动删除后继续。

Python 使用 [Python Build Standalone 固定发行版](https://github.com/astral-sh/python-build-standalone/releases/tag/20261003)。三个归档 URL、上游发布的 SHA-256、证书 wheel 和许可证源码包都记录在 `python-runtime.lock.json`；先验证摘要再解包。证书取自锁定的 certifi wheel，只复制 CA 文件和许可证，不运行 pip、不安装依赖。更新 Python/证书应显式更新锁文件并重新构建验收。

下载缓存默认在 `.build/desktop-cache/`，可用 `--cache-dir PATH` 指定。缓存每次都会检查摘要；错误缓存直接报错，不会静默接受或覆盖。缓存齐备且 Go 模块已下载时，资源准备不需要再次下载 Python。`--output PATH` 可写入单独验证目录。不要并行运行多个写入同一输出目录的准备命令。

## 资源约定

```text
resources/runtime/
├── core[.exe]
├── console[.exe]
├── config.json                  # 空对象，由程序应用默认业务配置
├── runtime-manifest.json        # 目标平台与锁定的 Python 版本
├── python/
│   ├── bin/python3              # macOS
│   ├── python.exe               # Windows（仅对应平台存在）
│   └── cacert.pem
├── scripts/                     # school/cat 与其共享模块，来自物化源码
└── licenses/                    # 项目、上游来源、Python 依赖和证书许可证
```

Python 原归档中的标准库、共享库和许可证保留。生产任务脚本使用明确文件列表，不带测试文件、shell 定时入口或账号 JSON。core 的工作目录为资源根；运行器使用绝对 Python 路径，并设置 `SSL_CERT_FILE` 指向随包证书、`PYTHONDONTWRITEBYTECODE=1`、`PYTHONNOUSERSITE=1`，避免写入资源或加载用户安装包。

## 验证

```bash
python3 -m unittest discover -s desktop/scripts -p 'test_*.py' -v
```

原生验收在对应平台的开发机执行，仍从仓库根目录开始；Windows 可将 `python3` 换成 `py -3.12`。以下资源准备不能用交叉编译结果代替；代码或补丁修改后应重新准备。Go 核心测试每次使用新的临时物化目录，避免测到旧补丁。

```bash
python3 desktop/scripts/prepare.py
python3 -c "import pathlib, subprocess, sys, tempfile; tmp=tempfile.TemporaryDirectory(prefix='wb2a-native-check-'); core=pathlib.Path(tmp.name)/'core'; subprocess.run([sys.executable,'scripts/overlay.py','prepare','--output',str(core)],check=True); subprocess.run(['go','-C',str(core),'test','./cmd/server','./internal/taskrun','./internal/durablefs','./internal/scheduler'],check=True)"
go -C console test ./...
node --test console/web_test.cjs
python3 -m unittest discover -s desktop/scripts -p 'test_*.py' -v
cargo test --manifest-path desktop/Cargo.toml --all-targets
cargo test --manifest-path desktop/Cargo.toml --test runtime -- --ignored
```

最后一条显式运行默认跳过的原生集成测试，使用临时应用数据、随机本机端口和空账号，检查启动、停止及重启后的密钥保留。Windows 尤其需要运行任务历史恢复和文件替换测试；这些测试在 macOS 上通过不能证明 Windows 文件共享语义通过。支持 CGO 的原生工具链还应执行 `go -C console test -race ./...`；普通 `go test` 不代表竞态检查完成。

打包单测使用临时目录和小型本地归档，不下载依赖、不读取真实凭据，覆盖摘要拒绝、解压越界拒绝、内部符号链接、三平台资源路径与发布后的目标清单、脚本范围，以及失败替换时旧资源保留。

原生目标准备时会实际运行随包 Python，导入 `ssl`、`hashlib`、`urllib.request` 和全部生产任务模块，并检查可加载 CA；只导入，不请求上游。交叉目标只完成构建和资源布局检查，输出明确的未执行提示。

单测、Python 导入验证和交叉编译都不等于 Windows/macOS 安装后端到端通过，也不证明真实账号授权或任务奖励到账。运行器、浏览器登录、退出清理和数据保留需由隔离集成验收补充记录。
