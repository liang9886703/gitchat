use serde::Deserialize;
use std::{
    fs,
    path::PathBuf,
    process::{Child, Command, Stdio},
    thread,
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};
use tauri::Manager;
use url::Url;

#[derive(Deserialize)]
struct Ready {
    url: String,
    pid: u32,
}

pub struct ServerProcess {
    child: Child,
    ready_file: PathBuf,
    pub url: Url,
}

impl ServerProcess {
    pub fn start(app: &tauri::AppHandle) -> Result<Self, String> {
        let data = app
            .path()
            .app_data_dir()
            .map_err(|e| e.to_string())?
            .join("server");
        fs::create_dir_all(&data).map_err(|e| e.to_string())?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(&data, fs::Permissions::from_mode(0o700))
                .map_err(|e| e.to_string())?;
        }
        let stamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos();
        let ready_file = data.join(format!("ready-{}-{stamp}.json", std::process::id()));
        let port_file = data.join("listen-port");
        let port = read_port(&port_file)?;
        let filename = if cfg!(windows) {
            "gitchat-server.exe"
        } else {
            "gitchat-server"
        };
        let executable = std::env::current_exe()
            .map_err(|e| e.to_string())?
            .parent()
            .ok_or("无法定位应用目录")?
            .join(filename);
        let executable = if executable.is_file() {
            executable
        } else if cfg!(debug_assertions) {
            // Cargo checks/tests need no sidecar. Local development prepares it
            // in the source directory using the exact host target suffix.
            let target = env!("GITCHAT_TARGET");
            let suffix = if cfg!(windows) { ".exe" } else { "" };
            PathBuf::from(env!("CARGO_MANIFEST_DIR"))
                .join("binaries")
                .join(format!("gitchat-server-{target}{suffix}"))
        } else {
            return Err("安装包缺少本地服务，请重新安装求知".into());
        };
        if !executable.is_file() {
            return Err("本地服务尚未构建，请运行 npm run prepare:desktop".into());
        }
        let log = fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(data.join("server.log"))
            .map_err(|e| e.to_string())?;
        let mut command = Command::new(executable);
        command
            .current_dir(&data)
            .env("GITCHAT_ADDR", format!("127.0.0.1:{}", port.unwrap_or(0)))
            .env("DATA_DIR", &data)
            .env("GITCHAT_READY_FILE", &ready_file)
            .env("GITCHAT_EXIT_ON_STDIN_CLOSE", "1")
            .env("GITCHAT_SECURE_COOKIES", "0")
            .env("GIN_MODE", "release")
            .stdin(Stdio::piped())
            .stdout(Stdio::null())
            .stderr(log);
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000); // CREATE_NO_WINDOW
        }
        let child = command
            .spawn()
            .map_err(|e| format!("无法启动本地服务：{e}"))?;
        let mut process = Self {
            child,
            ready_file,
            url: Url::parse("http://127.0.0.1").unwrap(),
        };
        let deadline = Instant::now() + Duration::from_secs(15);
        while Instant::now() < deadline {
            if let Some(status) = process.child.try_wait().map_err(|e| e.to_string())? {
                return Err(format!(
                    "本地服务退出（{status}）。请检查原本地端口是否被占用；为保护草稿，应用不会切换到其他端口。详情见应用数据目录中的 server.log"
                ));
            }
            if let Ok(bytes) = fs::read(&process.ready_file) {
                if let Ok(ready) = serde_json::from_slice::<Ready>(&bytes) {
                    let url = crate::policy::server_url(&ready.url)?;
                    if ready.pid != process.child.id() || !crate::policy::is_loopback(&url) {
                        return Err("本地服务启动信息不匹配".into());
                    }
                    let ready_port = url.port().ok_or("本地服务未返回有效端口")?;
                    if port.is_some_and(|value| value != ready_port) {
                        return Err("本地服务端口与已保存空间不匹配".into());
                    }
                    if port.is_none() {
                        let tmp = data.join("listen-port.tmp");
                        fs::write(&tmp, ready_port.to_string()).map_err(|e| e.to_string())?;
                        fs::rename(tmp, &port_file).map_err(|e| e.to_string())?;
                    }
                    process.url = url;
                    return Ok(process);
                }
            }
            thread::sleep(Duration::from_millis(50));
        }
        Err("本地服务启动超时，请检查数据目录是否被另一实例占用".into())
    }

    pub fn running(&mut self) -> bool {
        matches!(self.child.try_wait(), Ok(None))
    }

    pub fn stop(&mut self) {
        // Closing stdin lets Go cancel work and flush data. The pipe also closes
        // automatically if this native process crashes.
        self.child.stdin.take();
        let deadline = Instant::now() + Duration::from_secs(4);
        while Instant::now() < deadline {
            if !self.running() {
                break;
            }
            thread::sleep(Duration::from_millis(50));
        }
        if self.running() {
            let _ = self.child.kill();
        }
        let _ = self.child.wait();
        let _ = fs::remove_file(&self.ready_file);
    }
}

fn read_port(path: &std::path::Path) -> Result<Option<u16>, String> {
    match fs::read_to_string(path) {
        Ok(value) => value
            .trim()
            .parse::<u16>()
            .ok()
            .filter(|port| *port != 0)
            .map(Some)
            .ok_or_else(|| "本地端口设置损坏，请恢复备份；应用未更换端口以保留原空间".into()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(format!("无法读取本地端口设置：{error}")),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn saved_origin_is_reused_and_corruption_never_falls_back() {
        let path = std::env::temp_dir().join(format!(
            "gitchat-port-test-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        assert_eq!(read_port(&path).unwrap(), None);
        fs::write(&path, "41863").unwrap();
        assert_eq!(read_port(&path).unwrap(), Some(41863));
        for value in ["0", "65536", "invalid"] {
            fs::write(&path, value).unwrap();
            assert!(read_port(&path).is_err());
        }
        fs::remove_file(path).unwrap();
    }
}

impl Drop for ServerProcess {
    fn drop(&mut self) {
        self.stop();
    }
}
