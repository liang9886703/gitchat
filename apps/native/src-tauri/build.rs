fn main() {
    // NDK r27 needs explicit ELF alignment for Android's 16 KiB page devices.
    // Scope the flags to Android so desktop and iOS linkers stay unaffected.
    if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("android") {
        println!("cargo:rustc-link-arg=-Wl,-z,max-page-size=16384");
        println!("cargo:rustc-link-arg=-Wl,-z,common-page-size=16384");
    }
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
