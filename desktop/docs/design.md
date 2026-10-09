# 原生桌面运行器

用户已确认 Windows/macOS 原生自包含应用，源码位于 desktop/。仅托盘，无内嵌网页；首次启动无需下载依赖。启动现有 core 和 console，默认浏览器访问仅本机地址。全部六类任务保留，随包 Python 解释器及标准库。关闭浏览器不退出服务；托盘退出停止任务和全部子进程，保留数据。

应用名称为 `wb2api-desktop`；安装包采用 `wb2api-desktop-{系统}-{指令集}-{版本号}.*`，系统使用 `windows` / `macos`，指令集使用 `x64` / `arm64`，版本号取自 Tauri 配置。

托盘打开 `http://127.0.0.1:7863/?admin_key=...`，前端读取后清除 URL 参数并填入密码框，由用户点击登录；继续使用管理会话和 CSRF，已有会话可直接进入。不记录完整 URL，不保存密钥到 localStorage。仅本机页面接受密钥预填。

core、console 和 Python 使用对应平台原生文件。资源只读，数据写用户应用数据目录。首批 Windows x64、macOS arm64/x64；使用固定 Python 下载地址及 SHA-256，在构建期准备，用户运行时不下载。Windows 包采用离线 WebView2 安装配置，以免首次运行依赖网络；无 WebView 的减包需另经干净 Windows 验证。

单实例；端口占用明确报错，不结束他人进程。启动就绪后才打开浏览器，空账号不视为启动失败。桌面启动器清理继承的 WB2A_*、Python 路径变量并设置专用值。通过父进程 stdin 管道控制 Go 服务退出，关闭管道触发取消；Windows Job Object、Unix 进程组作为超时清理后盾。

运行器不使用真实凭据做测试，不重启真实服务、不发布或推送。上游快照不变，新增 core 文件放 extensions/，修改既有上游文件用 patches/。现有 Docker 入口保留。
