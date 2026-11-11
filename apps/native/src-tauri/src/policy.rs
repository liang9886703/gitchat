use url::Url;

/// The web application uses absolute /api and asset paths, so deployment URLs
/// must address a server origin, not an arbitrary path or a credentialed URL.
pub fn server_url(input: &str) -> Result<Url, String> {
    let url = Url::parse(input.trim()).map_err(|_| "请输入完整的 HTTPS 服务地址".to_owned())?;
    let host = url.host_str().ok_or("服务地址缺少主机名")?;
    if ["tauri.localhost", "ipc.localhost", "asset.localhost"].contains(&host) {
        return Err("这个地址由应用内部使用".into());
    }
    let loopback = is_loopback(&url);
    if url.scheme() != "https" && !(url.scheme() == "http" && loopback) {
        return Err("远程服务需要 HTTPS；HTTP 仅限本机回环地址".into());
    }
    if !url.username().is_empty() || url.password().is_some() {
        return Err("服务地址中不能包含用户名或密码".into());
    }
    if url.path() != "/" || url.query().is_some() || url.fragment().is_some() {
        return Err("请输入服务根地址，例如 https://qiuzhi.example.com".into());
    }
    Ok(url)
}

pub fn is_loopback(url: &Url) -> bool {
    match url.host() {
        Some(url::Host::Domain("localhost")) => true,
        Some(url::Host::Ipv4(ip)) => ip.is_loopback(),
        Some(url::Host::Ipv6(ip)) => ip.is_loopback(),
        _ => false,
    }
}

pub fn launcher_url(url: &Url) -> bool {
    if cfg!(any(target_os = "windows", target_os = "android")) {
        url.scheme() == "http" && url.host_str() == Some("tauri.localhost") && url.port().is_none()
    } else {
        url.scheme() == "tauri" && url.host_str() == Some("localhost") && url.port().is_none()
    }
}

pub fn same_origin(a: &Url, b: &Url) -> bool {
    a.origin() == b.origin()
}

pub fn external_url(url: &Url) -> bool {
    matches!(url.scheme(), "https" | "http")
        && !launcher_url(url)
        && url.username().is_empty()
        && url.password().is_none()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn accepts_secure_origins_and_loopback() {
        for input in [
            "https://example.com",
            "https://example.com:8443/",
            "http://127.0.0.1:8002",
            "http://localhost:8002",
            "http://[::1]:8002/",
        ] {
            assert!(server_url(input).is_ok(), "{input}");
        }
    }

    #[test]
    fn rejects_unsafe_or_ambiguous_server_addresses() {
        for input in [
            "http://example.com",
            "http://192.168.1.4",
            "file:///etc/passwd",
            "javascript:alert(1)",
            "tauri://localhost/",
            "https://tauri.localhost",
            "https://ipc.localhost",
            "https://user:pass@example.com",
            "https://example.com/api",
            "https://example.com/?token=secret",
            "https://example.com/#fragment",
            "127.0.0.1:8002",
            "https://example.com/%2e%2e/foo",
        ] {
            assert!(server_url(input).is_err(), "{input}");
        }
    }

    #[test]
    fn navigation_is_exact_origin_scoped() {
        let base = server_url("https://example.com").unwrap();
        assert!(same_origin(
            &base,
            &Url::parse("https://example.com/?share=123").unwrap()
        ));
        for input in [
            "https://example.com.evil.test",
            "http://example.com",
            "https://example.com:8443",
        ] {
            assert!(!same_origin(&base, &Url::parse(input).unwrap()));
        }
        assert!(!external_url(&Url::parse("file:///tmp/a").unwrap()));
        assert!(!external_url(&Url::parse("javascript:alert(1)").unwrap()));
        let internal = if cfg!(any(target_os = "windows", target_os = "android")) {
            "http://tauri.localhost/index.html"
        } else {
            "tauri://localhost/index.html"
        };
        assert!(launcher_url(&Url::parse(internal).unwrap()));
        assert!(!launcher_url(
            &Url::parse("https://tauri.localhost/index.html").unwrap()
        ));
        assert!(!launcher_url(&base));
    }
}
