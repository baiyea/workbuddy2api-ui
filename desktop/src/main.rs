#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::{
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc, Mutex,
    },
    thread,
    time::Duration,
};
use tauri::{
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    Manager,
};
use tauri_plugin_dialog::{DialogExt, MessageDialogKind};
use tauri_plugin_opener::OpenerExt;
use wb2api_desktop::runtime::{Runtime, RuntimePaths};

#[derive(Default)]
struct Control {
    runtime: Mutex<Option<Runtime>>,
    cancel: AtomicBool,
    exiting: AtomicBool,
    may_exit: AtomicBool,
}
fn show_error(app: &tauri::AppHandle, message: impl Into<String>) {
    app.dialog()
        .message(message)
        .title("wb2api-desktop")
        .kind(MessageDialogKind::Error)
        .show(|_| {});
}
fn open_console(app: &tauri::AppHandle) {
    let control = app.state::<Arc<Control>>();
    let url = control
        .runtime
        .try_lock()
        .ok()
        .and_then(|slot| slot.as_ref().map(|r| r.url().to_owned()));
    if let Some(url) = url {
        if app.opener().open_url(url, None::<&str>).is_err() {
            show_error(app, "无法打开默认浏览器，请检查系统默认浏览器设置。");
        }
    }
}
fn quit(app: &tauri::AppHandle) {
    let control = app.state::<Arc<Control>>().inner().clone();
    if control.exiting.swap(true, Ordering::AcqRel) {
        return;
    }
    control.cancel.store(true, Ordering::Release);
    let app = app.clone();
    thread::spawn(move || {
        if let Some(mut runtime) = control.runtime.lock().unwrap().take() {
            runtime.stop();
        }
        control.may_exit.store(true, Ordering::Release);
        app.exit(0);
    });
}
fn main() {
    let app=tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app,_,_|{open_console(app);}))
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_dialog::init())
        .manage(Arc::new(Control::default()))
        .setup(|app| {
            #[cfg(target_os="macos")]
            app.set_activation_policy(tauri::ActivationPolicy::Accessory);
            let status=MenuItem::with_id(app,"status","正在启动…",false,None::<&str>)?;
            let open=MenuItem::with_id(app,"open","打开控制台",false,None::<&str>)?;
            let exit=MenuItem::with_id(app,"exit","退出 wb2api-desktop",true,None::<&str>)?;
            let menu=Menu::with_items(app,&[&status,&open,&exit])?;
            TrayIconBuilder::with_id("workbuddy")
                .icon(app.default_window_icon().expect("bundled app icon").clone())
                .tooltip("wb2api-desktop · 本机网关")
                .menu(&menu)
                .on_menu_event(|app,event|match event.id.as_ref(){"open"=>open_console(app),"exit"=>quit(app),_=>{}})
                .build(app)?;
            let paths=RuntimePaths::new(app.path().resource_dir()?.join("runtime"), app.path().app_local_data_dir()?);
            let handle=app.handle().clone();let control=app.state::<Arc<Control>>().inner().clone();
            thread::spawn(move||{
                // Hold this lock throughout startup, so quit waits for cancelled startup cleanup.
                let mut slot=control.runtime.lock().unwrap();
                match Runtime::start(paths,7863,&control.cancel) {
                    Ok(runtime)=>{
                        if control.cancel.load(Ordering::Acquire) {drop(runtime);return;}
                        *slot=Some(runtime);drop(slot);
                        let _=status.set_text("运行中 · 仅本机访问");let _=open.set_enabled(true);
                        open_console(&handle);
                    }
                    Err(error)=>{
                        drop(slot);
                        if !control.cancel.load(Ordering::Acquire){let _=status.set_text("启动失败");show_error(&handle,error.to_string());}
                        return;
                    }
                }
                while !control.cancel.load(Ordering::Acquire) {
                    thread::sleep(Duration::from_secs(1));
                    let mut slot=control.runtime.lock().unwrap();
                    let failed=slot.as_mut().map(|runtime|!runtime.healthy().unwrap_or(false)).unwrap_or(false);
                    if failed {
                        // Keep cleanup serialized with Quit, but never wait for UI while locked.
                        if let Some(mut runtime)=slot.take(){runtime.stop();}
                        drop(slot);
                        let _=open.set_enabled(false);let _=status.set_text("服务已停止");
                        show_error(&handle,"后台服务意外退出。其他任务已停止，请退出后重新启动应用，并检查应用数据目录中的日志。");
                        break;
                    }
                }
            });
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("无法初始化 wb2api-desktop 托盘");
    app.run(|app, event| {
        if let tauri::RunEvent::ExitRequested { api, .. } = event {
            let control = app.state::<Arc<Control>>();
            if !control.may_exit.load(Ordering::Acquire) {
                api.prevent_exit();
                quit(app);
            }
        }
    });
}
