use serde::{Deserialize, Serialize};
use std::fs;
use std::io::{BufRead, BufReader, Read, Write};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use tauri::{
    ipc::Channel,
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

fn yzj_bridge_home() -> PathBuf {
    let home = std::env::var_os("USERPROFILE")
        .or_else(|| std::env::var_os("HOME"))
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("."));
    home.join(".yzj-bridge")
}

fn gui_prefs_path() -> PathBuf {
    yzj_bridge_home().join("gui-prefs.json")
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

fn clear_stale_token() {
    let _ = fs::remove_file(token_file());
}

fn find_bridge_bin(app: &AppHandle) -> Option<PathBuf> {
    let mut candidates: Vec<PathBuf> = Vec::new();
    // Dev first: prefer freshly built bridge/bin over stale src-tauri/binaries.
    candidates.push(PathBuf::from("../../bridge/bin/yzj-bridge.exe"));
    candidates.push(PathBuf::from("../bridge/bin/yzj-bridge.exe"));
    if let Ok(exe) = std::env::current_exe() {
        if let Some(dir) = exe.parent() {
            candidates.push(dir.join("yzj-bridge.exe"));
            candidates.push(dir.join("yzj-bridge"));
            candidates.push(dir.join("binaries").join("yzj-bridge.exe"));
            candidates.push(dir.join("bridge").join("yzj-bridge.exe"));
        }
    }
    if let Ok(res) = app.path().resource_dir() {
        candidates.push(res.join("yzj-bridge.exe"));
        candidates.push(res.join("binaries").join("yzj-bridge.exe"));
        candidates.push(res.join("bridge").join("yzj-bridge.exe"));
    }
    candidates.push(PathBuf::from("binaries/yzj-bridge.exe"));
    candidates.push(PathBuf::from("src-tauri/binaries/yzj-bridge.exe"));
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
    // Drop stale token from a crashed bridge so we don't probe the wrong process forever.
    clear_stale_token();
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
        "PATCH" => {
            let mut r = ureq::request("PATCH", &url).set("Authorization", &auth);
            if let Some(b) = body {
                r = r.set("Content-Type", "application/json");
                r.send_string(b)
            } else {
                r.call()
            }
        }
        "DELETE" => {
            let mut r = ureq::delete(&url).set("Authorization", &auth);
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

/// Stream SSE from control API chat endpoint; each event is forwarded to `on_event`.
/// Payload shape: `{ "event": "<type>", "data": <json-value-or-string> }`.
#[tauri::command]
async fn bridge_chat_stream(
    app: AppHandle,
    path: String,
    body: String,
    on_event: Channel<serde_json::Value>,
) -> Result<(), String> {
    let app2 = app.clone();
    tauri::async_runtime::spawn_blocking(move || {
        let st = app2.state::<BridgeState>();
        let _ = ensure_bridge_running(&app2, &st);
        do_chat_stream(&st, &path, &body, &on_event)
    })
    .await
    .map_err(|e| e.to_string())?
}

fn do_chat_stream(
    state: &BridgeState,
    path: &str,
    body: &str,
    on_event: &Channel<serde_json::Value>,
) -> Result<(), String> {
    let token = state.token.lock().unwrap().clone();
    let addr = state.addr.lock().unwrap().clone();
    let (token, addr) = if token.is_empty() {
        let ep = read_token_file().ok_or_else(|| "no token".to_string())?;
        (ep.token, ep.addr)
    } else {
        (token, addr)
    };
    let url = format!("http://{}{}", addr, path);
    let resp = ureq::post(&url)
        .set("Authorization", &format!("Bearer {}", token))
        .set("Content-Type", "application/json")
        .set("Accept", "text/event-stream")
        .send_string(body)
        .map_err(|e| match e {
            ureq::Error::Status(code, resp) => {
                let text = resp.into_string().unwrap_or_default();
                if text.trim().is_empty() {
                    format!("HTTP {code}")
                } else {
                    text
                }
            }
            other => other.to_string(),
        })?;
    let status = resp.status();
    if !(200..300).contains(&status) {
        let text = resp.into_string().unwrap_or_default();
        return Err(if text.trim().is_empty() {
            format!("HTTP {status}")
        } else {
            text
        });
    }

    let reader = BufReader::new(resp.into_reader());
    let mut event_name = String::from("message");
    let mut data_buf = String::new();

    let flush_event = |event_name: &str, data_buf: &str, on_event: &Channel<serde_json::Value>| -> Result<(), String> {
        if data_buf.is_empty() && event_name == "message" {
            return Ok(());
        }
        let data_val: serde_json::Value =
            serde_json::from_str(data_buf).unwrap_or_else(|_| serde_json::Value::String(data_buf.to_string()));
        let payload = serde_json::json!({
            "event": event_name,
            "data": data_val,
        });
        on_event.send(payload).map_err(|e| e.to_string())
    };

    for line in reader.lines() {
        let line = line.map_err(|e| e.to_string())?;
        if line.is_empty() {
            flush_event(&event_name, &data_buf, on_event)?;
            event_name = String::from("message");
            data_buf.clear();
            continue;
        }
        if line.starts_with(':') {
            continue; // comment
        }
        if let Some(rest) = line.strip_prefix("event:") {
            event_name = rest.trim().to_string();
            continue;
        }
        if let Some(rest) = line.strip_prefix("data:") {
            let piece = rest.strip_prefix(' ').unwrap_or(rest);
            if !data_buf.is_empty() {
                data_buf.push('\n');
            }
            data_buf.push_str(piece);
            continue;
        }
    }
    // trailing event without blank line
    flush_event(&event_name, &data_buf, on_event)?;
    Ok(())
}

#[tauri::command]
fn get_app_version(app: AppHandle) -> String {
    app.package_info().version.to_string()
}

const GITHUB_OWNER: &str = "JinRyu-online";
const GITHUB_REPO: &str = "yzj_bot_bridge";
const UPDATE_USER_AGENT: &str = "YZJBridge-Updater";

#[derive(Serialize, Deserialize, Default)]
struct UpdatePrefs {
    #[serde(default)]
    skipped_version: String,
}

#[derive(Serialize, Clone)]
#[serde(rename_all = "camelCase")]
struct UpdateCheckResult {
    available: bool,
    current_version: String,
    latest_version: String,
    notes: String,
    download_url: String,
    published_at: String,
    skipped: bool,
    /// Non-empty when check succeeded but update cannot be offered (e.g. missing installer).
    message: String,
}

#[derive(Deserialize)]
struct GhRelease {
    tag_name: String,
    body: Option<String>,
    published_at: Option<String>,
    assets: Vec<GhAsset>,
}

#[derive(Deserialize)]
struct GhAsset {
    name: String,
    browser_download_url: String,
}

fn update_prefs_path() -> PathBuf {
    yzj_bridge_home().join("update-prefs.json")
}

fn load_update_prefs() -> UpdatePrefs {
    let path = update_prefs_path();
    let Ok(raw) = fs::read_to_string(&path) else {
        return UpdatePrefs::default();
    };
    serde_json::from_str(&raw).unwrap_or_default()
}

fn save_update_prefs(prefs: &UpdatePrefs) -> Result<(), String> {
    let path = update_prefs_path();
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(|e| format!("创建更新偏好目录失败: {e}"))?;
    }
    let raw = serde_json::to_string_pretty(prefs).map_err(|e| e.to_string())?;
    fs::write(&path, raw).map_err(|e| format!("写入更新偏好失败: {e}"))
}

fn normalize_version(raw: &str) -> String {
    raw.trim().trim_start_matches('v').trim_start_matches('V').to_string()
}

fn parse_version_tuple(raw: &str) -> Option<(u64, u64, u64)> {
    let core = normalize_version(raw);
    let core = core.split('-').next().unwrap_or(&core);
    let mut parts = core.split('.');
    let major = parts.next()?.parse().ok()?;
    let minor = parts.next().unwrap_or("0").parse().unwrap_or(0);
    let patch = parts.next().unwrap_or("0").parse().unwrap_or(0);
    Some((major, minor, patch))
}

fn version_is_newer(latest: &str, current: &str) -> bool {
    match (parse_version_tuple(latest), parse_version_tuple(current)) {
        (Some(l), Some(c)) => l > c,
        _ => false,
    }
}

fn is_versioned_setup_asset(name: &str, latest_version: &str) -> bool {
    let expected = format!("YZJBridge-{latest_version}-Windows-x64-setup.exe");
    name.eq_ignore_ascii_case(&expected)
}

fn pick_setup_download_url(assets: &[GhAsset], latest_version: &str) -> Option<String> {
    assets
        .iter()
        .find(|a| is_versioned_setup_asset(&a.name, latest_version))
        .map(|a| a.browser_download_url.clone())
}

fn is_allowed_download_url(url: &str) -> bool {
    let lower = url.to_ascii_lowercase();
    if !lower.starts_with("https://") {
        return false;
    }
    let rest = &lower["https://".len()..];
    let host = rest.split('/').next().unwrap_or("");
    let host = host.rsplit('@').next().unwrap_or(host);
    matches!(
        host,
        "github.com"
            | "www.github.com"
            | "objects.githubusercontent.com"
            | "release-assets.githubusercontent.com"
            | "github-releases.githubusercontent.com"
    )
}

fn fetch_latest_release() -> Result<GhRelease, String> {
    let url = format!(
        "https://api.github.com/repos/{GITHUB_OWNER}/{GITHUB_REPO}/releases/latest"
    );
    let resp = ureq::get(&url)
        .set("User-Agent", UPDATE_USER_AGENT)
        .set("Accept", "application/vnd.github+json")
        .call()
        .map_err(|e| format!("请求 GitHub Releases 失败: {e}"))?;
    let status = resp.status();
    let text = resp
        .into_string()
        .map_err(|e| format!("读取 GitHub 响应失败: {e}"))?;
    if !(200..300).contains(&status) {
        return Err(format!("GitHub API HTTP {status}: {text}"));
    }
    serde_json::from_str(&text).map_err(|e| format!("解析 GitHub Release 失败: {e}"))
}

fn fetch_update_candidate(current_version: &str) -> Result<UpdateCheckResult, String> {
    let release = fetch_latest_release()?;
    let latest_version = normalize_version(&release.tag_name);
    let current_version = normalize_version(current_version);
    let notes = release.body.unwrap_or_default();
    let published_at = release.published_at.unwrap_or_default();
    let download_url = pick_setup_download_url(&release.assets, &latest_version).unwrap_or_default();
    let prefs = load_update_prefs();
    let skipped = !prefs.skipped_version.is_empty()
        && normalize_version(&prefs.skipped_version) == latest_version;
    Ok(UpdateCheckResult {
        available: false,
        current_version,
        latest_version,
        notes,
        download_url,
        published_at,
        skipped,
        message: String::new(),
    })
}

/// Decide visibility: force=true still shows a previously skipped version.
fn apply_update_visibility(
    mut result: UpdateCheckResult,
    force: bool,
) -> UpdateCheckResult {
    let newer = version_is_newer(&result.latest_version, &result.current_version);
    let has_url = !result.download_url.is_empty();
    if newer && !has_url {
        result.available = false;
        result.message = format!(
            "发现新版本 v{}，但 Release 中缺少 YZJBridge-{}-Windows-x64-setup.exe",
            result.latest_version, result.latest_version
        );
        return result;
    }
    result.available = newer && has_url && (force || !result.skipped);
    result.message = String::new();
    result
}

#[tauri::command]
async fn check_for_update(app: AppHandle, force: bool) -> Result<UpdateCheckResult, String> {
    let current = app.package_info().version.to_string();
    tauri::async_runtime::spawn_blocking(move || {
        let raw = fetch_update_candidate(&current)?;
        Ok(apply_update_visibility(raw, force))
    })
    .await
    .map_err(|e| e.to_string())?
}

#[tauri::command]
async fn set_skipped_update_version(version: String) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || {
        let mut prefs = load_update_prefs();
        prefs.skipped_version = normalize_version(&version);
        save_update_prefs(&prefs)
    })
    .await
    .map_err(|e| e.to_string())?
}

fn download_update_installer(download_url: &str) -> Result<PathBuf, String> {
    if !is_allowed_download_url(download_url) {
        return Err("下载地址不在允许的域名白名单内".into());
    }
    let resp = ureq::get(download_url)
        .set("User-Agent", UPDATE_USER_AGENT)
        .call()
        .map_err(|e| format!("下载更新包失败: {e}"))?;
    let status = resp.status();
    if !(200..300).contains(&status) {
        return Err(format!("下载更新包 HTTP {status}"));
    }
    let dest = std::env::temp_dir().join("YZJBridge-update-setup.exe");
    let mut reader = resp.into_reader();
    let mut file = fs::File::create(&dest).map_err(|e| format!("创建临时安装包失败: {e}"))?;
    let mut buf = [0u8; 64 * 1024];
    loop {
        let n = reader
            .read(&mut buf)
            .map_err(|e| format!("读取更新包失败: {e}"))?;
        if n == 0 {
            break;
        }
        file.write_all(&buf[..n])
            .map_err(|e| format!("写入临时安装包失败: {e}"))?;
    }
    file.flush()
        .map_err(|e| format!("刷新临时安装包失败: {e}"))?;
    Ok(dest)
}

fn launch_installer(path: &PathBuf) -> Result<(), String> {
    let mut cmd = Command::new(path);
    cmd.stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    cmd.spawn()
        .map_err(|e| format!("启动安装器失败: {e}"))?;
    Ok(())
}

#[tauri::command]
async fn download_and_launch_update(
    app: AppHandle,
    state: State<'_, BridgeState>,
    download_url: String,
) -> Result<(), String> {
    let path = tauri::async_runtime::spawn_blocking(move || download_update_installer(&download_url))
        .await
        .map_err(|e| e.to_string())??;
    launch_installer(&path)?;
    let _ = do_fetch(&state, "POST", "/v1/shutdown", None);
    app.exit(0);
    Ok(())
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
async fn open_path_default(path: String) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || open_path_default_sync(&path))
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

fn open_path_default_sync(path: &str) -> Result<(), String> {
    let path = path.trim();
    if path.is_empty() {
        return Err("路径为空".into());
    }
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        Command::new("cmd")
            .args(["/C", "start", "", path])
            .creation_flags(CREATE_NO_WINDOW)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|e| e.to_string())?;
        return Ok(());
    }
    #[cfg(not(windows))]
    {
        let opener = if cfg!(target_os = "macos") { "open" } else { "xdg-open" };
        Command::new(opener)
            .arg(path)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|e| e.to_string())?;
        Ok(())
    }
}

/// Open a visible terminal with the official Cursor/Claude install command.
/// User confirms with Enter before the installer runs.
fn open_cli_install_terminal_sync(engine: &str) -> Result<(), String> {
    let engine = engine.trim().to_lowercase();
    let (title, command) = match engine.as_str() {
        "cursor" | "cursor_cli" | "agent" => (
            "YZJ Bridge · 安装 Cursor CLI",
            "irm 'https://cursor.com/install?win32=true' | iex",
        ),
        "claude" | "claude_code" => (
            "YZJ Bridge · 安装 Claude Code",
            "irm https://claude.ai/install.ps1 | iex",
        ),
        _ => return Err(format!("未知引擎: {engine}")),
    };

    #[cfg(windows)]
    {
        let ps = format!(
            "$Host.UI.RawUI.WindowTitle = {title}; \
Write-Host {title} -ForegroundColor Cyan; \
Write-Host ''; \
Write-Host '将执行官方安装命令:' -ForegroundColor Yellow; \
Write-Host {cmd} -ForegroundColor White; \
Write-Host ''; \
Read-Host '按 Enter 开始安装（Ctrl+C 取消）' | Out-Null; \
Write-Host ''; \
Write-Host '安装中…' -ForegroundColor Cyan; \
{raw}; \
Write-Host ''; \
Write-Host '若安装成功，请回到 YZJ Bridge → AI 设置 → 重新扫描' -ForegroundColor Green; \
Read-Host '按 Enter 关闭窗口' | Out-Null",
            title = ps_single_quote(title),
            cmd = ps_single_quote(command),
            raw = command,
        );
        Command::new("powershell.exe")
            .args([
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-NoExit",
                "-Command",
                &ps,
            ])
            .spawn()
            .map_err(|e| format!("无法打开 PowerShell: {e}"))?;
        return Ok(());
    }

    #[cfg(not(windows))]
    {
        let _ = (title, command);
        Err("当前仅 Windows 支持一键打开安装终端".into())
    }
}

fn ps_single_quote(s: &str) -> String {
    format!("'{}'", s.replace('\'', "''"))
}

#[tauri::command]
async fn open_cli_install_terminal(engine: String) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || open_cli_install_terminal_sync(&engine))
        .await
        .map_err(|e| e.to_string())?
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
        .plugin(tauri_plugin_dialog::init())
        .manage(BridgeState {
            child: Mutex::new(None),
            token: Mutex::new(String::new()),
            addr: Mutex::new(String::new()),
            start_lock: Mutex::new(()),
        })
        .invoke_handler(tauri::generate_handler![
            get_app_version,
            check_for_update,
            set_skipped_update_version,
            download_and_launch_update,
            get_endpoint,
            ensure_bridge,
            bridge_fetch,
            bridge_chat_stream,
            get_autostart,
            set_autostart,
            reveal_path,
            open_path_default,
            open_cli_install_terminal,
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
