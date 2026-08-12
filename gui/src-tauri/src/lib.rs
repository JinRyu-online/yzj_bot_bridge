use serde::{Deserialize, Serialize};
use std::fs;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use tauri::{
    menu::{Menu, MenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Manager, State, WindowEvent,
};

struct BridgeState {
    child: Mutex<Option<Child>>,
    token: Mutex<String>,
    addr: Mutex<String>,
    /// Serialize bridge process startup to avoid double-spawn races.
    start_lock: Mutex<()>,
}

#[derive(Serialize, Deserialize)]
struct Endpoint {
    addr: String,
    token: String,
}

#[derive(Serialize, Deserialize, Clone)]
struct GuiPrefs {
    /// When true, closing the window hides to tray; when false, exits and stops the bridge.
    #[serde(default = "default_true")]
    close_to_tray: bool,
}

fn default_true() -> bool {
    true
}

impl Default for GuiPrefs {
    fn default() -> Self {
        Self {
            close_to_tray: true,
        }
    }
}

fn gui_prefs_path() -> PathBuf {
    let home = std::env::var_os("USERPROFILE")
        .or_else(|| std::env::var_os("HOME"))
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("."));
    home.join(".yzj-bridge").join("gui-prefs.json")
}

fn load_gui_prefs() -> GuiPrefs {
    let path = gui_prefs_path();
    let Ok(raw) = fs::read_to_string(&path) else {
        return GuiPrefs::default();
    };
    serde_json::from_str(&raw).unwrap_or_default()
}

fn save_gui_prefs(prefs: &GuiPrefs) -> Result<(), String> {
    let path = gui_prefs_path();
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(|e| format!("创建偏好目录失败: {e}"))?;
    }
    let raw = serde_json::to_string_pretty(prefs).map_err(|e| e.to_string())?;
    fs::write(&path, raw).map_err(|e| format!("写入偏好失败: {e}"))
}

fn token_file() -> PathBuf {
    std::env::temp_dir().join("yzj-bridge.token")
}

fn read_token_file() -> Option<Endpoint> {
    let raw = fs::read_to_string(token_file()).ok()?;
    let mut lines = raw.lines();
    let token = lines.next()?.trim().to_string();
    let addr = lines
        .next()
        .unwrap_or("127.0.0.1:18765")
        .trim()
        .to_string();
    if token.is_empty() {
        return None;
    }
    Some(Endpoint { addr, token })
}

fn find_bridge_bin(app: &AppHandle) -> Option<PathBuf> {
    let mut candidates: Vec<PathBuf> = Vec::new();
    if let Ok(exe) = std::env::current_exe() {
        if let Some(dir) = exe.parent() {
            candidates.push(dir.join("yzj-bridge.exe"));
            candidates.push(dir.join("yzj-bridge"));
            candidates.push(dir.join("bridge").join("yzj-bridge.exe"));
        }
    }
    if let Ok(res) = app.path().resource_dir() {
        candidates.push(res.join("yzj-bridge.exe"));
        candidates.push(res.join("bridge").join("yzj-bridge.exe"));
    }
    candidates.push(PathBuf::from("binaries/yzj-bridge.exe"));
    candidates.push(PathBuf::from("src-tauri/binaries/yzj-bridge.exe"));
    // Dev: repo bridge/bin
    candidates.push(PathBuf::from("../bridge/bin/yzj-bridge.exe"));
    candidates.push(PathBuf::from("../../bridge/bin/yzj-bridge.exe"));
    candidates.push(PathBuf::from("E:/cursor-cli-robot/bridge/bin/yzj-bridge.exe"));
    candidates.into_iter().find(|p| p.exists())
}

fn ensure_bridge_running(app: &AppHandle, state: &BridgeState) -> Result<Endpoint, String> {
    if let Some(ep) = healthy_endpoint(state) {
        return Ok(ep);
    }
    let _guard = state
        .start_lock
        .lock()
        .map_err(|_| "bridge start lock poisoned".to_string())?;
    // Re-check after acquiring lock (another starter may have finished).
    if let Some(ep) = healthy_endpoint(state) {
        return Ok(ep);
    }

    let bin = find_bridge_bin(app).ok_or_else(|| {
        "找不到 yzj-bridge.exe，请先构建 bridge/bin/yzj-bridge.exe".to_string()
    })?;
    let mut cmd = Command::new(&bin);
    cmd.arg("--control-addr")
        .arg("127.0.0.1:18765")
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        const CREATE_NEW_PROCESS_GROUP: u32 = 0x0000_0200;
        cmd.creation_flags(CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP);
    }
    let mut child = cmd.spawn().map_err(|e| format!("启动桥失败: {e}"))?;
    // Wait for control API (up to ~15s). Bridge may still be loading config.
    for _ in 0..150 {
        std::thread::sleep(std::time::Duration::from_millis(100));
        if let Some(ep) = read_token_file() {
            if probe_health(&ep).is_ok() {
                *state.child.lock().unwrap() = Some(child);
                *state.token.lock().unwrap() = ep.token.clone();
                *state.addr.lock().unwrap() = ep.addr.clone();
                return Ok(ep);
            }
        }
    }
    let _ = child.kill();
    Err("桥进程启动超时，请检查 yzj-bridge.exe 与配置".into())
}

fn healthy_endpoint(state: &BridgeState) -> Option<Endpoint> {
    if let Some(ep) = read_token_file() {
        if probe_health(&ep).is_ok() {
            *state.token.lock().unwrap() = ep.token.clone();
            *state.addr.lock().unwrap() = ep.addr.clone();
            return Some(ep);
        }
    }
    let token = state.token.lock().unwrap().clone();
    let addr = state.addr.lock().unwrap().clone();
    if !token.is_empty() && !addr.is_empty() {
        let ep = Endpoint { addr, token };
        if probe_health(&ep).is_ok() {
            return Some(ep);
        }
    }
    None
}

fn probe_health(ep: &Endpoint) -> Result<(), String> {
    let url = format!("http://{}/health?token={}", ep.addr, ep.token);
    let resp = ureq::get(&url).call().map_err(|e| e.to_string())?;
    if resp.status() >= 200 && resp.status() < 300 {
        Ok(())
    } else {
        Err(format!("health {}", resp.status()))
    }
}

#[tauri::command]
fn get_endpoint(state: State<BridgeState>) -> Result<Endpoint, String> {
    let token = state.token.lock().unwrap().clone();
    let addr = state.addr.lock().unwrap().clone();
    if token.is_empty() {
        return read_token_file().ok_or_else(|| "bridge not ready".into());
    }
    Ok(Endpoint { addr, token })
}

#[tauri::command]
async fn ensure_bridge(app: AppHandle) -> Result<Endpoint, String> {
    let app2 = app.clone();
    tauri::async_runtime::spawn_blocking(move || {
        let st = app2.state::<BridgeState>();
        ensure_bridge_running(&app2, &st)
    })
    .await
    .map_err(|e| e.to_string())?
}

fn do_fetch(state: &BridgeState, method: &str, path: &str, body: Option<&str>) -> Result<String, String> {
    let token = state.token.lock().unwrap().clone();
    let addr = state.addr.lock().unwrap().clone();
    let (token, addr) = if token.is_empty() {
        let ep = read_token_file().ok_or_else(|| "no token".to_string())?;
        (ep.token, ep.addr)
    } else {
        (token, addr)
    };
    let url = format!("http://{}{}", addr, path);
    let auth = format!("Bearer {}", token);
    let result = match method.to_uppercase().as_str() {
        "GET" => ureq::get(&url).set("Authorization", &auth).call(),
        "POST" => {
            let mut r = ureq::post(&url).set("Authorization", &auth);
            if let Some(b) = body {
                r = r.set("Content-Type", "application/json");
                r.send_string(b)
            } else {
                r.call()
            }
        }
        "PUT" => {
            let mut r = ureq::put(&url).set("Authorization", &auth);
            if let Some(b) = body {
                r = r.set("Content-Type", "application/json");
                r.send_string(b)
            } else {
                r.call()
            }
        }
        other => return Err(format!("unsupported method {other}")),
    };
    match result {
        Ok(resp) => {
            let status = resp.status();
            let text = resp.into_string().map_err(|e| e.to_string())?;
            if !(200..300).contains(&status) {
                return Err(if text.trim().is_empty() {
                    format!("HTTP {status}")
                } else {
                    text
                });
            }
            Ok(text)
        }
        Err(ureq::Error::Status(code, resp)) => {
            let text = resp.into_string().unwrap_or_default();
            Err(if text.trim().is_empty() {
                format!("HTTP {code}")
            } else {
                text
            })
        }
        Err(e) => Err(e.to_string()),
    }
}

#[tauri::command]
async fn bridge_fetch(
    app: AppHandle,
    method: String,
    path: String,
    body: Option<String>,
) -> Result<String, String> {
    let app2 = app.clone();
    tauri::async_runtime::spawn_blocking(move || {
        let st = app2.state::<BridgeState>();
        let _ = ensure_bridge_running(&app2, &st);
        do_fetch(&st, &method, &path, body.as_deref())
    })
    .await
    .map_err(|e| e.to_string())?
}

#[tauri::command]
async fn get_autostart() -> bool {
    tauri::async_runtime::spawn_blocking(is_autostart_enabled)
        .await
        .unwrap_or(false)
}

#[tauri::command]
async fn set_autostart(enabled: bool) -> Result<bool, String> {
    // Run off the UI/async worker so antivirus prompts cannot freeze the webview.
    tauri::async_runtime::spawn_blocking(move || set_autostart_sync(enabled))
        .await
        .map_err(|e| e.to_string())?
}

fn is_autostart_enabled() -> bool {
    registry_run_value().is_some() || autostart_cmd_path().map(|p| p.exists()).unwrap_or(false)
}

fn set_autostart_sync(enabled: bool) -> Result<bool, String> {
    // Prefer HKCU Run via winreg (no console flash). Startup *.cmd is often flagged by AV.
    let _ = remove_autostart_cmd();
    #[cfg(windows)]
    {
        use winreg::enums::*;
        use winreg::RegKey;
        let hkcu = RegKey::predef(HKEY_CURRENT_USER);
        let (key, _) = hkcu
            .create_subkey(r"Software\Microsoft\Windows\CurrentVersion\Run")
            .map_err(|e| format!("打开 Run 注册表失败: {e}"))?;
        if enabled {
            let exe = std::env::current_exe().map_err(|e| e.to_string())?;
            let value = format!("\"{}\"", exe.to_string_lossy());
            key.set_value("YZJBridge", &value)
                .map_err(|e| format!("写入开机自启失败: {e}"))?;
        } else {
            let _ = key.delete_value("YZJBridge");
        }
        return Ok(enabled);
    }
    #[cfg(not(windows))]
    {
        let _ = enabled;
        Err("仅 Windows 支持开机自启".into())
    }
}

fn registry_run_value() -> Option<String> {
    #[cfg(windows)]
    {
        use winreg::enums::*;
        use winreg::RegKey;
        let hkcu = RegKey::predef(HKEY_CURRENT_USER);
        let key = hkcu
            .open_subkey(r"Software\Microsoft\Windows\CurrentVersion\Run")
            .ok()?;
        let value: String = key.get_value("YZJBridge").ok()?;
        if value.is_empty() {
            None
        } else {
            Some(value)
        }
    }
    #[cfg(not(windows))]
    {
        None
    }
}

#[tauri::command]
async fn reveal_path(path: String) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || reveal_path_sync(&path))
        .await
        .map_err(|e| e.to_string())?
}

#[tauri::command]
async fn get_close_to_tray() -> bool {
    tauri::async_runtime::spawn_blocking(|| load_gui_prefs().close_to_tray)
        .await
        .unwrap_or(true)
}

#[tauri::command]
async fn set_close_to_tray(enabled: bool) -> Result<bool, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let mut prefs = load_gui_prefs();
        prefs.close_to_tray = enabled;
        save_gui_prefs(&prefs)?;
        Ok(enabled)
    })
    .await
    .map_err(|e| e.to_string())?
}

fn reveal_path_sync(path: &str) -> Result<(), String> {
    if path.is_empty() {
        return Err("路径为空".into());
    }
    let p = PathBuf::from(path);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        let mut cmd = if p.is_file() {
            let mut c = Command::new("explorer");
            c.arg(format!("/select,{}", p.to_string_lossy()));
            c
        } else {
            let dir = if p.is_dir() {
                p.clone()
            } else {
                p.parent().map(|x| x.to_path_buf()).unwrap_or(p.clone())
            };
            let mut c = Command::new("explorer");
            c.arg(dir);
            c
        };
        cmd.creation_flags(CREATE_NO_WINDOW)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|e| e.to_string())?;
        return Ok(());
    }
    #[cfg(not(windows))]
    {
        let _ = p;
        Err("仅 Windows 支持打开资源管理器".into())
    }
}

fn remove_autostart_cmd() -> Result<(), String> {
    if let Some(path) = autostart_cmd_path() {
        if path.exists() {
            fs::remove_file(&path).map_err(|e| e.to_string())?;
        }
    }
    Ok(())
}

fn autostart_cmd_path() -> Option<PathBuf> {
    let appdata = std::env::var_os("APPDATA")?;
    Some(
        PathBuf::from(appdata)
            .join("Microsoft")
            .join("Windows")
            .join("Start Menu")
            .join("Programs")
            .join("Startup")
            .join("YZJBridge.cmd"),
    )
}

fn show_main(app: &AppHandle) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.set_focus();
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .manage(BridgeState {
            child: Mutex::new(None),
            token: Mutex::new(String::new()),
            addr: Mutex::new(String::new()),
            start_lock: Mutex::new(()),
        })
        .invoke_handler(tauri::generate_handler![
            get_endpoint,
            ensure_bridge,
            bridge_fetch,
            get_autostart,
            set_autostart,
            reveal_path,
            get_close_to_tray,
            set_close_to_tray
        ])
        .setup(|app| {
            let show_i = MenuItem::with_id(app, "show", "打开主界面", true, None::<&str>)?;
            let start_i = MenuItem::with_id(app, "wss_start", "启动全部 WSS", true, None::<&str>)?;
            let stop_i = MenuItem::with_id(app, "wss_stop", "停止全部 WSS", true, None::<&str>)?;
            let quit_i = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&show_i, &start_i, &stop_i, &quit_i])?;

            let mut tray = TrayIconBuilder::new()
                .menu(&menu)
                .tooltip("云之家机器人桥接")
                .show_menu_on_left_click(false);
            if let Some(icon) = app.default_window_icon() {
                tray = tray.icon(icon.clone());
            }
            let _tray = tray
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "show" => show_main(app),
                    "wss_start" => {
                        let app = app.clone();
                        std::thread::spawn(move || {
                            let state = app.state::<BridgeState>();
                            let _ = do_fetch(&state, "POST", "/v1/wss/start", None);
                        });
                    }
                    "wss_stop" => {
                        let app = app.clone();
                        std::thread::spawn(move || {
                            let state = app.state::<BridgeState>();
                            let _ = do_fetch(&state, "POST", "/v1/wss/stop", None);
                        });
                    }
                    "quit" => {
                        let app = app.clone();
                        std::thread::spawn(move || {
                            let state = app.state::<BridgeState>();
                            let _ = do_fetch(&state, "POST", "/v1/shutdown", None);
                            app.exit(0);
                        });
                    }
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if let TrayIconEvent::Click {
                        button: MouseButton::Left,
                        button_state: MouseButtonState::Up,
                        ..
                    } = event
                    {
                        show_main(tray.app_handle());
                    }
                })
                .build(app)?;

            // Start bridge off the UI thread so tray setup isn't blocked / frozen.
            let handle = app.handle().clone();
            std::thread::spawn(move || {
                let state = handle.state::<BridgeState>();
                let _ = ensure_bridge_running(&handle, &state);
            });

            Ok(())
        })
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                if load_gui_prefs().close_to_tray {
                    api.prevent_close();
                    let _ = window.hide();
                } else {
                    api.prevent_close();
                    let app = window.app_handle().clone();
                    std::thread::spawn(move || {
                        let state = app.state::<BridgeState>();
                        let _ = do_fetch(&state, "POST", "/v1/shutdown", None);
                        app.exit(0);
                    });
                }
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
