use serde::Deserialize;
#[cfg(unix)]
use std::process::{Child, Stdio};
use std::{
    env,
    fs::{self, File, OpenOptions},
    io::{self, BufRead, BufReader, Write},
    net::{Ipv4Addr, SocketAddr, TcpListener, TcpStream},
    path::PathBuf,
    process::Command,
    sync::atomic::{AtomicBool, Ordering},
    thread,
    time::{Duration, Instant},
};

const START_TIMEOUT: Duration = Duration::from_secs(30);
const STOP_TIMEOUT: Duration = Duration::from_secs(10);

#[derive(Clone)]
pub struct RuntimePaths {
    pub resources: PathBuf,
    pub data: PathBuf,
}
impl RuntimePaths {
    pub fn new(resources: PathBuf, data: PathBuf) -> Self {
        Self { resources, data }
    }
    pub fn python(&self) -> PathBuf {
        self.resources.join(if cfg!(windows) {
            "python/python.exe"
        } else {
            "python/bin/python3"
        })
    }
    fn program(&self, name: &str) -> PathBuf {
        self.resources.join(format!(
            "{}{}",
            name,
            if cfg!(windows) { ".exe" } else { "" }
        ))
    }
    fn validate(&self) -> io::Result<()> {
        for path in [
            self.program("core"),
            self.program("console"),
            self.python(),
            self.resources.join("python/cacert.pem"),
            self.resources.join("scripts/task_runner.py"),
            self.resources.join("scripts/school_open_day_2026.py"),
            self.resources.join("config.json"),
        ] {
            if !path.is_file() {
                return Err(io::Error::new(
                    io::ErrorKind::NotFound,
                    "运行资源不完整，请重新安装应用",
                ));
            }
        }
        for dir in [
            &self.data,
            &self.data.join("auths"),
            &self.data.join("data"),
            &self.data.join("keys"),
            &self.data.join("logs"),
        ] {
            fs::create_dir_all(dir)?;
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt;
                fs::set_permissions(dir, fs::Permissions::from_mode(0o700))?;
            }
        }
        Ok(())
    }
}

pub fn login_url(port: u16, key: &str) -> io::Result<String> {
    if key.is_empty() || port == 0 {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "无效的本机登录地址",
        ));
    }
    let mut url =
        url::Url::parse(&format!("http://127.0.0.1:{port}/")).expect("constant loopback URL");
    url.query_pairs_mut().append_pair("admin_key", key);
    Ok(url.into())
}

// Environment overrides are private to children. A shell or user Python installation is never used.
pub fn child_command(
    paths: &RuntimePaths,
    name: &str,
    core_port: u16,
    web_port: u16,
) -> io::Result<Command> {
    if name != "core" && name != "console" {
        return Err(io::Error::new(io::ErrorKind::InvalidInput, "未知服务"));
    }
    let mut cmd = Command::new(paths.program(name));
    for (key, _) in env::vars_os() {
        let upper = key.to_string_lossy().to_ascii_uppercase();
        if upper.starts_with("WB2A_")
            || upper.starts_with("PYTHON")
            || ["SSL_CERT_FILE", "SSL_CERT_DIR"].contains(&upper.as_str())
        {
            cmd.env_remove(key);
        }
    }
    cmd.current_dir(&paths.resources)
        .env("WB2A_DESKTOP", "true")
        .env(
            "WB2A_LISTEN",
            format!(
                "127.0.0.1:{}",
                if name == "core" { core_port } else { web_port }
            ),
        )
        .env("TZ", "Asia/Shanghai");
    if name == "core" {
        cmd.args(["-config"])
            .arg(paths.resources.join("config.json"))
            .env("WB2A_CORE", "true")
            .env("WB2A_AUTH_DIR", paths.data.join("auths"))
            .env("WB2A_STATE_FILE", paths.data.join("data/state.json"))
            .env("WB2A_KEY_DIR", paths.data.join("keys"))
            .env(
                "WB2A_DEVICE_TOKEN_FILE",
                paths.data.join("data/device_token"),
            )
            .env("WB2A_PYTHON", paths.python())
            .env("PYTHONNOUSERSITE", "1")
            .env("PYTHONDONTWRITEBYTECODE", "1")
            .env("PYTHONUTF8", "1")
            .env("SSL_CERT_FILE", paths.resources.join("python/cacert.pem"));
    } else {
        cmd.env("WB2A_CORE_URL", format!("http://127.0.0.1:{core_port}"))
            .env("WB2A_KEY_FILE", paths.data.join("keys/keys.json"));
    }
    Ok(cmd)
}

fn service_log(paths: &RuntimePaths, name: &str) -> io::Result<File> {
    let path = paths.data.join("logs").join(format!("{name}.log"));
    if path.exists() {
        let old = path.with_extension("previous.log");
        if old.exists() {
            fs::remove_file(&old)?;
        }
        fs::rename(&path, old)?;
    }
    let mut options = OpenOptions::new();
    options.create(true).append(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }
    options.open(path)
}

pub struct ManagedChild {
    #[cfg(unix)]
    child: Child,
    #[cfg(windows)]
    child: crate::windows_process::ChildProcess,
    stopped: bool,
}
impl ManagedChild {
    pub fn spawn(command: Command, log: File) -> io::Result<Self> {
        #[cfg(unix)]
        let child = {
            use std::os::unix::process::CommandExt;
            let mut command = command;
            command
                .stdin(Stdio::piped())
                .stdout(log.try_clone()?)
                .stderr(log)
                .process_group(0);
            command.spawn()?
        };
        #[cfg(windows)]
        let child = crate::windows_process::ChildProcess::spawn(&command, log)?;
        Ok(Self {
            child,
            stopped: false,
        })
    }
    pub fn id(&self) -> u32 {
        self.child.id()
    }
    pub fn running(&mut self) -> io::Result<bool> {
        #[cfg(unix)]
        {
            Ok(self.child.try_wait()?.is_none())
        }
        #[cfg(windows)]
        {
            self.child.running()
        }
    }
    pub fn request_stop(&mut self) {
        #[cfg(unix)]
        {
            self.child.stdin.take();
        }
        #[cfg(windows)]
        self.child.close_stdin();
    }
    fn finish(&mut self) {
        if self.stopped {
            return;
        }
        // Clean the process group even if its direct child has already exited.
        #[cfg(unix)]
        {
            unsafe {
                libc::kill(-(self.child.id() as i32), libc::SIGKILL);
            }
            let _ = self.child.kill();
            let _ = self.child.wait();
        }
        #[cfg(windows)]
        self.child.kill_wait();
        self.stopped = true;
    }
    pub fn stop(&mut self, timeout: Duration) {
        if self.stopped {
            return;
        }
        self.request_stop();
        let deadline = Instant::now() + timeout;
        while self.running().unwrap_or(false) && Instant::now() < deadline {
            thread::sleep(Duration::from_millis(25));
        }
        self.finish();
    }
}
impl Drop for ManagedChild {
    fn drop(&mut self) {
        self.stop(STOP_TIMEOUT);
    }
}

#[derive(Deserialize)]
struct Keys {
    admin_key: String,
    bridge_key: String,
}
fn read_keys(paths: &RuntimePaths) -> io::Result<Keys> {
    let file = File::open(paths.data.join("keys/keys.json"))?;
    if file.metadata()?.len() > 4096 {
        return Err(io::Error::other("密钥文件无效"));
    }
    let keys: Keys = serde_json::from_reader(file).map_err(|_| io::Error::other("密钥文件无效"))?;
    if keys.admin_key.len() < 32 || keys.bridge_key.len() < 32 {
        return Err(io::Error::other("密钥文件无效"));
    }
    Ok(keys)
}

fn http_ready(port: u16, path: &str, key: Option<&str>) -> bool {
    let addr = SocketAddr::from((Ipv4Addr::LOCALHOST, port));
    let Ok(mut socket) = TcpStream::connect_timeout(&addr, Duration::from_millis(300)) else {
        return false;
    };
    let _ = socket.set_read_timeout(Some(Duration::from_millis(500)));
    let _ = socket.set_write_timeout(Some(Duration::from_millis(500)));
    let auth = key
        .map(|key| format!("Authorization: Bearer {key}\r\n"))
        .unwrap_or_default();
    if write!(
        socket,
        "GET {path} HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\n{auth}Connection: close\r\n\r\n"
    )
    .is_err()
    {
        return false;
    }
    // Only inspect a bounded status line; no response body or credential is logged.
    let mut reader = BufReader::new(socket);
    let mut line = Vec::new();
    use std::io::Read;
    reader
        .by_ref()
        .take(128)
        .read_until(b'\n', &mut line)
        .is_ok()
        && line.starts_with(b"HTTP/1.1 200 ")
}

pub struct Runtime {
    core: ManagedChild,
    web: Option<ManagedChild>,
    url: String,
    stopped: bool,
}
impl Runtime {
    pub fn start(paths: RuntimePaths, web_port: u16, cancel: &AtomicBool) -> io::Result<Self> {
        paths.validate()?;
        let web_guard = TcpListener::bind((Ipv4Addr::LOCALHOST, web_port)).map_err(|_| {
            io::Error::new(
                io::ErrorKind::AddrInUse,
                format!("本机端口 {web_port} 已占用，请关闭占用它的程序后重试"),
            )
        })?;
        let actual_web_port = web_guard.local_addr()?.port();
        let core_guard = TcpListener::bind((Ipv4Addr::LOCALHOST, 0))?;
        let core_port = core_guard.local_addr()?.port();
        let command = child_command(&paths, "core", core_port, actual_web_port)?;
        let log = service_log(&paths, "core")?;
        drop(core_guard);
        let core = ManagedChild::spawn(command, log)?;
        let mut runtime = Self {
            core,
            web: None,
            url: String::new(),
            stopped: false,
        };
        let deadline = Instant::now() + START_TIMEOUT;
        let keys = loop {
            runtime.check_start(cancel, deadline)?;
            if let Ok(keys) = read_keys(&paths) {
                if http_ready(core_port, "/internal/v1/status", Some(&keys.bridge_key)) {
                    break keys;
                }
            }
            thread::sleep(Duration::from_millis(100));
        };
        let command = child_command(&paths, "console", core_port, actual_web_port)?;
        let log = service_log(&paths, "console")?;
        drop(web_guard);
        runtime.web = Some(ManagedChild::spawn(command, log)?);
        loop {
            runtime.check_start(cancel, deadline)?;
            if http_ready(actual_web_port, "/livez", None) {
                break;
            }
            thread::sleep(Duration::from_millis(100));
        }
        // Recheck processes after readiness: never open a pre-existing listener after an early child exit.
        thread::sleep(Duration::from_millis(100));
        runtime.check_start(cancel, deadline)?;
        runtime.url = login_url(actual_web_port, &keys.admin_key)?;
        Ok(runtime)
    }
    fn check_start(&mut self, cancel: &AtomicBool, deadline: Instant) -> io::Result<()> {
        if cancel.load(Ordering::Acquire) {
            return Err(io::Error::new(io::ErrorKind::Interrupted, "启动已取消"));
        }
        if Instant::now() > deadline {
            return Err(io::Error::new(
                io::ErrorKind::TimedOut,
                "服务启动超时，请检查本地日志",
            ));
        }
        if !self.healthy()? {
            return Err(io::Error::other("服务提前退出，请检查本地日志"));
        }
        Ok(())
    }
    pub fn url(&self) -> &str {
        &self.url
    }
    pub fn healthy(&mut self) -> io::Result<bool> {
        Ok(!self.stopped
            && self.core.running()?
            && match self.web.as_mut() {
                Some(web) => web.running()?,
                None => true,
            })
    }
    pub fn stop(&mut self) {
        if self.stopped {
            return;
        }
        self.core.request_stop();
        if let Some(web) = self.web.as_mut() {
            web.request_stop();
        }
        let deadline = Instant::now() + STOP_TIMEOUT;
        while Instant::now() < deadline {
            let core_done = !self.core.running().unwrap_or(false);
            let web_done = self
                .web
                .as_mut()
                .map(|w| !w.running().unwrap_or(false))
                .unwrap_or(true);
            if core_done && web_done {
                break;
            }
            thread::sleep(Duration::from_millis(25));
        }
        if let Some(web) = self.web.as_mut() {
            web.finish();
        }
        self.core.finish();
        self.url.clear();
        self.stopped = true;
    }
}
impl Drop for Runtime {
    fn drop(&mut self) {
        self.stop();
    }
}
