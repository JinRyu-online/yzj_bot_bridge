fn main() {
    // MinGW ld 会把依赖里的全局符号全导出，超过 PE 65535 序数上限报
    // "export ordinal too large"。桌面端不需要这些导出。
    if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("windows")
        && std::env::var("CARGO_CFG_TARGET_ENV").as_deref() == Ok("gnu")
    {
        println!("cargo::rustc-link-arg=-Wl,--exclude-libs=ALL,--exclude-all-symbols");
    }
    tauri_build::build()
}
