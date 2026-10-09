fn main() {
    let manifest = "resources/runtime/runtime-manifest.json";
    println!("cargo:rerun-if-changed={manifest}");
    if std::env::var("PROFILE").as_deref() == Ok("release") {
        let contents = std::fs::read_to_string(manifest)
            .expect("先运行 python3 desktop/scripts/prepare.py 准备本平台运行资源");
        let value: serde_json::Value = serde_json::from_str(&contents).expect("运行资源清单无效");
        assert_eq!(
            value["target"].as_str(),
            std::env::var("TARGET").ok().as_deref(),
            "运行资源架构与 Rust 目标不一致，请按 --target 重新准备运行资源"
        );
    }
    tauri_build::build()
}
