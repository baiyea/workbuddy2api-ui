use std::path::PathBuf;
use wb2api_desktop::runtime::{child_command, login_url, RuntimePaths};

#[test]
fn login_url_encodes_key_without_changing_authority_or_path() {
    let url = login_url(7863, "a+b /?#&=密钥").unwrap();
    let parsed = url::Url::parse(&url).unwrap();
    assert_eq!(parsed.host_str(), Some("127.0.0.1"));
    assert_eq!(parsed.port(), Some(7863));
    assert_eq!(parsed.path(), "/");
    assert_eq!(
        parsed.query_pairs().collect::<Vec<_>>(),
        vec![("admin_key".into(), "a+b /?#&=密钥".into())]
    );
    assert!(login_url(7863, "").is_err());
}

#[test]
fn children_receive_private_paths_and_explicit_python_environment() {
    let paths = RuntimePaths::new(
        PathBuf::from("/bundle/runtime"),
        PathBuf::from("/user/data"),
    );
    let command = child_command(&paths, "core", 17864, 17863).unwrap();
    let env: std::collections::HashMap<_, _> = command
        .get_envs()
        .map(|(k, v)| {
            (
                k.to_string_lossy().into_owned(),
                v.map(|v| v.to_string_lossy().into_owned()),
            )
        })
        .collect();
    assert_eq!(env["WB2A_LISTEN"].as_deref(), Some("127.0.0.1:17864"));
    assert_eq!(env["WB2A_DESKTOP"].as_deref(), Some("true"));
    assert!(PathBuf::from(env["WB2A_PYTHON"].as_deref().unwrap())
        .starts_with(PathBuf::from("/bundle/runtime").join("python")));
    assert_eq!(
        PathBuf::from(env["WB2A_AUTH_DIR"].as_deref().unwrap()),
        PathBuf::from("/user/data").join("auths")
    );
    assert_eq!(env["PYTHONNOUSERSITE"].as_deref(), Some("1"));
    assert_eq!(env["PYTHONDONTWRITEBYTECODE"].as_deref(), Some("1"));
}

#[test]
fn console_is_bound_to_loopback_and_receives_matching_core_and_key_paths() {
    let paths = RuntimePaths::new("/bundle/runtime".into(), "/user/data".into());
    let cmd = child_command(&paths, "console", 17864, 17863).unwrap();
    let env: std::collections::HashMap<_, _> = cmd
        .get_envs()
        .filter_map(|(k, v)| {
            v.map(|v| {
                (
                    k.to_string_lossy().into_owned(),
                    v.to_string_lossy().into_owned(),
                )
            })
        })
        .collect();
    assert_eq!(env["WB2A_CORE_URL"], "http://127.0.0.1:17864");
    assert_eq!(env["WB2A_LISTEN"], "127.0.0.1:17863");
    assert_eq!(
        PathBuf::from(&env["WB2A_KEY_FILE"]),
        PathBuf::from("/user/data").join("keys/keys.json")
    );
    assert!(child_command(&paths, "arbitrary-shell", 1, 2).is_err());
}

fn temp_dir(label: &str) -> PathBuf {
    let stamp = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_nanos();
    let dir =
        std::env::temp_dir().join(format!("wb-desktop-{label}-{}-{stamp}", std::process::id()));
    std::fs::create_dir_all(&dir).unwrap();
    dir
}

#[test]
fn child_fixture() {
    if let Ok(marker) = std::env::var("WB_DESKTOP_TEST_MARKER") {
        use std::io::Read;
        let mut input = Vec::new();
        std::io::stdin().read_to_end(&mut input).unwrap();
        std::fs::write(marker, "stopped").unwrap();
    }
}

#[test]
fn stopping_child_closes_stdin_and_waits_for_cleanup() {
    use wb2api_desktop::runtime::ManagedChild;
    let dir = temp_dir("stop");
    let marker = dir.join("done");
    let mut cmd = std::process::Command::new(std::env::current_exe().unwrap());
    cmd.args(["--exact", "child_fixture", "--nocapture"])
        .env("WB_DESKTOP_TEST_MARKER", &marker)
        .stdout(std::process::Stdio::null());
    let mut child =
        ManagedChild::spawn(cmd, std::fs::File::create(dir.join("child.log")).unwrap()).unwrap();
    child.stop(std::time::Duration::from_secs(3));
    assert_eq!(std::fs::read_to_string(&marker).unwrap(), "stopped");
    assert!(!child.running().unwrap());
    child.stop(std::time::Duration::ZERO);
    std::fs::remove_dir_all(dir).unwrap();
}

#[test]
#[ignore = "requires prepared native runtime; run after desktop/scripts/prepare.py"]
fn bundled_runtime_starts_without_accounts_restarts_with_same_keys_and_stops() {
    use wb2api_desktop::runtime::Runtime;
    let dir = temp_dir("smoke");
    let paths = RuntimePaths::new(
        std::env::var_os("WB_DESKTOP_TEST_RESOURCES")
            .map(PathBuf::from)
            .unwrap_or_else(|| PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("resources/runtime")),
        dir.clone(),
    );
    let cancel = std::sync::atomic::AtomicBool::new(false);
    let mut runtime = Runtime::start(paths.clone(), 0, &cancel).unwrap();
    assert!(runtime.healthy().unwrap());
    let first_key = url::Url::parse(runtime.url())
        .unwrap()
        .query_pairs()
        .find(|(k, _)| k == "admin_key")
        .unwrap()
        .1
        .into_owned();
    assert!(first_key.len() >= 32);
    let parsed = url::Url::parse(runtime.url()).unwrap();
    let port = parsed.port().unwrap();
    let (headers, body) = request(port, "GET", "/", "", None);
    assert!(headers.starts_with("HTTP/1.0 200"));
    assert!(body.contains("桌面运行"));
    assert!(!body.contains("docker compose logs"));
    let payload = serde_json::json!({"key":first_key}).to_string();
    let (headers, body) = request(port, "POST", "/admin/login", &payload, None);
    assert!(headers.starts_with("HTTP/1.0 200"), "login failed");
    assert!(serde_json::from_str::<serde_json::Value>(&body).unwrap()["csrf"].is_string());
    let cookie = headers
        .lines()
        .find(|l| l.to_ascii_lowercase().starts_with("set-cookie:"))
        .unwrap()
        .split_once(':')
        .unwrap()
        .1
        .trim()
        .split(';')
        .next()
        .unwrap();
    let (headers, body) = request(port, "GET", "/admin/tasks", "", Some(cookie));
    assert!(headers.starts_with("HTTP/1.0 200"), "task catalog failed");
    let tasks: serde_json::Value = serde_json::from_str(&body).unwrap();
    assert_eq!(tasks["items"].as_array().unwrap().len(), 6);
    runtime.stop();
    assert!(!runtime.healthy().unwrap());
    let mut second = Runtime::start(paths, 0, &cancel).unwrap();
    let key = url::Url::parse(second.url())
        .unwrap()
        .query_pairs()
        .find(|(k, _)| k == "admin_key")
        .unwrap()
        .1
        .into_owned();
    assert_eq!(first_key, key);
    second.stop();
    std::fs::remove_dir_all(dir).unwrap();
}

fn request(
    port: u16,
    method: &str,
    path: &str,
    body: &str,
    cookie: Option<&str>,
) -> (String, String) {
    use std::io::{Read, Write};
    let mut stream = std::net::TcpStream::connect(("127.0.0.1", port)).unwrap();
    stream
        .set_read_timeout(Some(std::time::Duration::from_secs(3)))
        .unwrap();
    let cookie = cookie
        .map(|v| format!("Cookie: {v}\r\n"))
        .unwrap_or_default();
    write!(stream,"{method} {path} HTTP/1.0\r\nHost: 127.0.0.1:{port}\r\nOrigin: http://127.0.0.1:{port}\r\nContent-Type: application/json\r\n{cookie}Content-Length: {}\r\n\r\n{body}",body.len()).unwrap();
    let mut text = String::new();
    stream.read_to_string(&mut text).unwrap();
    let (headers, body) = text.split_once("\r\n\r\n").unwrap();
    (headers.into(), body.into())
}

#[test]
#[allow(clippy::zombie_processes)] // The fixture deliberately delegates descendant cleanup to the launcher.
fn process_tree_fixture() {
    let Ok(role) = std::env::var("WB_DESKTOP_TEST_ROLE") else {
        return;
    };
    if role == "grandchild" {
        std::thread::sleep(std::time::Duration::from_secs(60));
        return;
    }
    let child = std::process::Command::new(std::env::current_exe().unwrap())
        .args(["--exact", "process_tree_fixture"])
        .env("WB_DESKTOP_TEST_ROLE", "grandchild")
        .stdin(std::process::Stdio::null())
        .spawn()
        .unwrap();
    std::fs::write(
        std::env::var_os("WB_DESKTOP_TEST_PID").unwrap(),
        child.id().to_string(),
    )
    .unwrap();
    if role == "stubborn" {
        std::thread::sleep(std::time::Duration::from_secs(60));
    } else {
        use std::io::Read;
        let mut bytes = Vec::new();
        let _ = std::io::stdin().read_to_end(&mut bytes);
    }
    // Deliberately leave the grandchild alive. The launcher owns tree cleanup.
}

fn process_running(pid: u32) -> bool {
    #[cfg(unix)]
    {
        unsafe { libc::kill(pid as i32, 0) == 0 }
    }
    #[cfg(windows)]
    {
        use windows_sys::Win32::{
            Foundation::{CloseHandle, WAIT_TIMEOUT},
            System::Threading::{OpenProcess, WaitForSingleObject},
        };
        unsafe {
            let handle = OpenProcess(0x00100000, 0, pid);
            if handle.is_null() {
                return false;
            }
            let running = WaitForSingleObject(handle, 0) == WAIT_TIMEOUT;
            CloseHandle(handle);
            running
        }
    }
}

#[test]
fn shutdown_collects_grandchildren_even_after_direct_child_exits_or_hangs() {
    use wb2api_desktop::runtime::ManagedChild;
    for role in ["parent", "stubborn"] {
        let dir = temp_dir("tree");
        let pid_file = dir.join("pid");
        let mut command = std::process::Command::new(std::env::current_exe().unwrap());
        command
            .args(["--exact", "process_tree_fixture"])
            .env("WB_DESKTOP_TEST_ROLE", role)
            .env("WB_DESKTOP_TEST_PID", &pid_file);
        let mut child = ManagedChild::spawn(
            command,
            std::fs::File::create(dir.join("child.log")).unwrap(),
        )
        .unwrap();
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(5);
        let pid = loop {
            if let Ok(value) = std::fs::read_to_string(&pid_file) {
                if let Ok(pid) = value.parse::<u32>() {
                    break pid;
                }
            }
            assert!(
                std::time::Instant::now() < deadline,
                "child did not produce pid"
            );
            std::thread::sleep(std::time::Duration::from_millis(20));
        };
        child.stop(std::time::Duration::from_millis(150));
        assert!(!child.running().unwrap());
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(3);
        while process_running(pid) && std::time::Instant::now() < deadline {
            std::thread::sleep(std::time::Duration::from_millis(20));
        }
        assert!(!process_running(pid), "grandchild outlived managed tree");
        std::fs::remove_dir_all(dir).unwrap();
    }
}
