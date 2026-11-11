fn main() {
    println!(
        "cargo:rustc-env=GITCHAT_TARGET={}",
        std::env::var("TARGET").expect("Cargo target")
    );
    tauri_build::try_build(tauri_build::Attributes::new().app_manifest(
        tauri_build::AppManifest::new().commands(&[
            "connection_info",
            "connect_remote",
            "connect_local",
        ]),
    ))
    .expect("failed to build native application permissions");
}
