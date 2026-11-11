mod policy;
#[cfg(desktop)]
mod sidecar;

use serde::{Deserialize, Serialize};
use std::{
    fs,
    sync::{
        atomic::{AtomicU64, Ordering},
        Mutex,
    },
    time::Duration,
};
use tauri::{Manager, WebviewUrl, WebviewWindow, WebviewWindowBuilder};
use tauri_plugin_opener::OpenerExt;
use url::Url;

#[derive(Clone, Default, Deserialize, Serialize)]
struct Connection {
    kind: String,
    url: String,
}

#[derive(Default)]
struct Inner {
    endpoint: Option<Url>,
    connecting: bool,
    error: Option<String>,
    #[cfg(desktop)]
    sidecar: Option<sidecar::ServerProcess>,
}

#[derive(Default)]
struct AppState {
    inner: Mutex<Inner>,
    loading: AtomicU64,
}

#[derive(Serialize)]
struct ConnectionInfo {
    desktop: bool,
    previous: Option<Connection>,
    error: Option<String>,
}

fn local_command(window: &WebviewWindow) -> Result<(), String> {
    if window.label() != "main" || !policy::launcher_url(&window.url().map_err(|e| e.to_string())?)
    {
        return Err("该操作只允许在应用连接页面执行".into());
    }
    Ok(())
}

#[tauri::command]
fn connection_info(
    app: tauri::AppHandle,
    window: WebviewWindow,
    state: tauri::State<AppState>,
) -> Result<ConnectionInfo, String> {
    local_command(&window)?;
    let path = app
        .path()
        .app_config_dir()
        .map_err(|e| e.to_string())?
        .join("connection.json");
    let previous = fs::read(path)
        .ok()
        .and_then(|bytes| serde_json::from_slice::<Connection>(&bytes).ok())
        .filter(|connection| {
            connection.kind == "local" || policy::server_url(&connection.url).is_ok()
        });
    Ok(ConnectionInfo {
        desktop: cfg!(desktop),
        previous,
        error: state
            .inner
            .lock()
            .map_err(|_| "连接状态不可用")?
            .error
            .take(),
    })
}

fn save_connection(app: &tauri::AppHandle, value: &Connection) -> Result<(), String> {
    let dir = app.path().app_config_dir().map_err(|e| e.to_string())?;
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    let tmp = dir.join("connection.json.tmp");
    fs::write(&tmp, serde_json::to_vec(value).map_err(|e| e.to_string())?)
        .map_err(|e| e.to_string())?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(&tmp, fs::Permissions::from_mode(0o600)).map_err(|e| e.to_string())?;
    }
    fs::rename(tmp, dir.join("connection.json")).map_err(|e| e.to_string())
}

async fn check_server(url: &Url) -> Result<(), String> {
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(10))
        .redirect(reqwest::redirect::Policy::none())
        .build()
        .map_err(|_| "无法建立安全连接")?;
    let mut response = client
        .get(url.join("api/config").map_err(|_| "服务地址无效")?)
        .send()
        .await
        .map_err(|_| "无法连接服务，请检查地址、网络与 HTTPS 证书")?;
    if !response.status().is_success() {
        return Err(format!(
            "服务暂时不可用（HTTP {}）",
            response.status().as_u16()
        ));
    }
    if response
        .content_length()
        .is_some_and(|length| length > 64 * 1024)
    {
        return Err("服务返回的配置过大".into());
    }
    let mut bytes = Vec::new();
    while let Some(chunk) = response.chunk().await.map_err(|_| "无法读取服务配置")? {
        if bytes.len() + chunk.len() > 64 * 1024 {
            return Err("服务返回的配置过大".into());
        }
        bytes.extend_from_slice(&chunk);
    }
    let config: serde_json::Value =
        serde_json::from_slice(&bytes).map_err(|_| "这个地址没有返回求知服务配置")?;
    if config["mode"].as_str().is_none() || !config["apps"].is_array() {
        return Err("这个地址不是可用的求知服务".into());
    }
    Ok(())
}

fn begin_connection(state: &AppState) -> Result<(), String> {
    let mut inner = state.inner.lock().map_err(|_| "连接状态不可用")?;
    if inner.connecting {
        return Err("正在连接，请稍候".into());
    }
    inner.connecting = true;
    Ok(())
}

fn finish_connection(state: &AppState) {
    if let Ok(mut inner) = state.inner.lock() {
        inner.connecting = false;
    }
}

#[tauri::command]
async fn connect_remote(
    app: tauri::AppHandle,
    window: WebviewWindow,
    state: tauri::State<'_, AppState>,
    url: String,
) -> Result<(), String> {
    local_command(&window)?;
    let endpoint = policy::server_url(&url)?;
    begin_connection(&state)?;
    let result = async {
        check_server(&endpoint).await?;
        save_connection(
            &app,
            &Connection {
                kind: "remote".into(),
                url: endpoint.to_string(),
            },
        )?;
        {
            let mut inner = state.inner.lock().map_err(|_| "连接状态不可用")?;
            #[cfg(desktop)]
            if let Some(mut process) = inner.sidecar.take() {
                process.stop();
            }
            inner.endpoint = Some(endpoint.clone());
        }
        arm_load_watchdog(&app);
        window
            .navigate(endpoint)
            .map_err(|_| "无法打开服务页面".to_owned())
    }
    .await;
    finish_connection(&state);
    result
}

#[tauri::command]
async fn connect_local(
    app: tauri::AppHandle,
    window: WebviewWindow,
    state: tauri::State<'_, AppState>,
) -> Result<(), String> {
    local_command(&window)?;
    #[cfg(mobile)]
    {
        let _ = (app, state);
        return Err("手机端请连接 HTTPS 求知服务".into());
    }
    #[cfg(desktop)]
    {
        begin_connection(&state)?;
        let result = async {
            let existing = {
                let mut inner = state.inner.lock().map_err(|_| "连接状态不可用")?;
                let url = inner.sidecar.as_mut().and_then(|process| {
                    if process.running() {
                        Some(process.url.clone())
                    } else {
                        None
                    }
                });
                if url.is_none() {
                    inner.sidecar = None;
                }
                url
            };
            let endpoint = if let Some(url) = existing {
                url
            } else {
                let app_copy = app.clone();
                let process = tauri::async_runtime::spawn_blocking(move || {
                    sidecar::ServerProcess::start(&app_copy)
                })
                .await
                .map_err(|_| "本地服务启动失败")??;
                let url = process.url.clone();
                state.inner.lock().map_err(|_| "连接状态不可用")?.sidecar = Some(process);
                url
            };
            check_server(&endpoint).await?;
            save_connection(
                &app,
                &Connection {
                    kind: "local".into(),
                    url: String::new(),
                },
            )?;
            state.inner.lock().map_err(|_| "连接状态不可用")?.endpoint = Some(endpoint.clone());
            arm_load_watchdog(&app);
            window
                .navigate(endpoint)
                .map_err(|_| "无法打开本地服务".to_owned())
        }
        .await;
        finish_connection(&state);
        result
    }
}

fn arm_load_watchdog(app: &tauri::AppHandle) {
    let sequence = app
        .state::<AppState>()
        .loading
        .fetch_add(1, Ordering::SeqCst)
        + 1;
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(Duration::from_secs(20)).await;
        if app.state::<AppState>().loading.load(Ordering::SeqCst) == sequence {
            show_launcher(&app, Some("页面加载超时，请检查服务与网络后重试".into()));
        }
    });
}

#[cfg(desktop)]
fn request_menu_action(app: &tauri::AppHandle, action: &str) {
    if let Some(window) = app.get_webview_window("main") {
        if window.url().is_ok_and(|url| policy::launcher_url(&url)) {
            if action == "reload" {
                let _ = window.reload();
            } else {
                show_launcher(app, None);
            }
            return;
        }
        // The page owns draft/active-generation confirmation. No user data or
        // arbitrary script is interpolated into this fixed event dispatch.
        let script = if action == "reload" {
            "window.dispatchEvent(new CustomEvent('gitchat:native-request',{detail:{action:'reload'}}))"
        } else {
            "window.dispatchEvent(new CustomEvent('gitchat:native-request',{detail:{action:'settings'}}))"
        };
        if window.eval(script).is_err() {
            show_launcher(app, Some("页面无法响应，已返回连接设置".into()));
        }
    }
}

fn show_launcher(app: &tauri::AppHandle, error: Option<String>) {
    let state = app.state::<AppState>();
    state.loading.fetch_add(1, Ordering::SeqCst);
    if let Ok(mut inner) = state.inner.lock() {
        inner.error = error;
    }
    if let Some(window) = app.get_webview_window("main") {
        // App URLs use a different scheme on the WebView2/Android backends.
        let url = if cfg!(any(target_os = "windows", target_os = "android")) {
            "http://tauri.localhost/index.html?settings=1"
        } else {
            "tauri://localhost/index.html?settings=1"
        };
        let _ = window.navigate(Url::parse(url).expect("static launcher URL"));
        let _ = window.set_focus();
    }
}

const NATIVE_BRIDGE: &str = r#"
(() => {
  if (!['http:', 'https:'].includes(location.protocol) || location.hostname === 'tauri.localhost') return;
  window.__GITCHAT_NATIVE__ = true;
  const ready = () => {
    document.documentElement.dataset.native = 'true';
    window.dispatchEvent(new Event('gitchat:native-ready'));
  };
  document.addEventListener('DOMContentLoaded', ready, {once:true});
  if (document.readyState !== 'loading') ready();
  // Route external target=_blank links through native navigation policy on
  // every platform, including mobile where on_new_window is unsupported.
  document.addEventListener('click', event => {
    const anchor = event.target.closest?.('a[href]');
    if (anchor?.target === '_blank') {
      event.preventDefault();
      location.href = anchor.href;
    }
  }, true);
})();
"#;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let builder = tauri::Builder::default();
    #[cfg(desktop)]
    let builder = builder.plugin(tauri_plugin_single_instance::init(|app, _, _| {
        if let Some(window) = app.get_webview_window("main") {
            let _ = window.set_focus();
        }
    }));
    let app = builder
        .plugin(tauri_plugin_opener::init())
        .manage(AppState::default())
        .invoke_handler(tauri::generate_handler![
            connection_info,
            connect_remote,
            connect_local
        ])
        .setup(|app| {
            let nav_app = app.handle().clone();
            let load_app = app.handle().clone();
            let builder =
                WebviewWindowBuilder::new(app, "main", WebviewUrl::App("index.html".into()))
                    .title("求知 · GitChat")
                    .inner_size(1180.0, 820.0)
                    .min_inner_size(360.0, 520.0)
                    .initialization_script(NATIVE_BRIDGE)
                    .on_navigation(move |url| {
                        if policy::launcher_url(url) {
                            return true;
                        }
                        if url.scheme() == "gitchat" && url.host_str() == Some("settings") {
                            let app = nav_app.clone();
                            let _ = nav_app.run_on_main_thread(move || show_launcher(&app, None));
                            return false;
                        }
                        let allowed = nav_app
                            .state::<AppState>()
                            .inner
                            .lock()
                            .ok()
                            .and_then(|inner| {
                                inner
                                    .endpoint
                                    .as_ref()
                                    .map(|base| policy::same_origin(base, url))
                            })
                            .unwrap_or(false);
                        if allowed {
                            // This is before the network request; WebKit's
                            // PageLoadEvent::Started fires only after commit.
                            arm_load_watchdog(&nav_app);
                            return true;
                        }
                        if policy::external_url(url) {
                            let _ = nav_app.opener().open_url(url.as_str(), None::<&str>);
                        }
                        false
                    })
                    .on_page_load(move |_window, payload| {
                        let state = load_app.state::<AppState>();
                        if payload.event() == tauri::webview::PageLoadEvent::Finished {
                            state.loading.fetch_add(1, Ordering::SeqCst);
                        }
                    });
            #[cfg(desktop)]
            let builder = {
                let open_app = app.handle().clone();
                builder.on_new_window(move |url, _| {
                    if policy::external_url(&url) {
                        let _ = open_app.opener().open_url(url.as_str(), None::<&str>);
                    }
                    tauri::webview::NewWindowResponse::Deny
                })
            };
            let window = builder.build()?;
            #[cfg(desktop)]
            {
                use tauri::menu::{MenuBuilder, MenuItem, PredefinedMenuItem, SubmenuBuilder};
                let connect = MenuItem::with_id(
                    app,
                    "connection",
                    "连接与本地空间…",
                    true,
                    Some("CmdOrCtrl+,"),
                )?;
                let reload =
                    MenuItem::with_id(app, "reload", "重新加载", true, Some("CmdOrCtrl+R"))?;
                let menu = MenuBuilder::new(app)
                    .item(
                        &SubmenuBuilder::new(app, "求知")
                            .item(&connect)
                            .item(&reload)
                            .item(&PredefinedMenuItem::separator(app)?)
                            .item(&PredefinedMenuItem::quit(app, Some("退出求知"))?)
                            .build()?,
                    )
                    .item(
                        &SubmenuBuilder::new(app, "编辑")
                            .item(&PredefinedMenuItem::undo(app, None)?)
                            .item(&PredefinedMenuItem::redo(app, None)?)
                            .item(&PredefinedMenuItem::separator(app)?)
                            .item(&PredefinedMenuItem::cut(app, None)?)
                            .item(&PredefinedMenuItem::copy(app, None)?)
                            .item(&PredefinedMenuItem::paste(app, None)?)
                            .item(&PredefinedMenuItem::select_all(app, None)?)
                            .build()?,
                    )
                    .build()?;
                app.set_menu(menu)?;
                app.on_menu_event(|app, event| match event.id().as_ref() {
                    "connection" => request_menu_action(app, "settings"),
                    "reload" => request_menu_action(app, "reload"),
                    _ => {}
                });
                let handle = app.handle().clone();
                window.on_window_event(move |event| {
                    if matches!(event, tauri::WindowEvent::CloseRequested { .. }) {
                        handle.exit(0);
                    }
                });
            }
            #[cfg(mobile)]
            let _ = window;
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("failed to build 求知 native client");
    app.run(|app, event| {
        if let tauri::RunEvent::Exit = event {
            #[cfg(desktop)]
            if let Ok(mut inner) = app.state::<AppState>().inner.lock() {
                if let Some(mut process) = inner.sidecar.take() {
                    process.stop();
                }
            }
        }
    });
}
