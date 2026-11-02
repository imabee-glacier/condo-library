// CI passes the tag-derived release version as CI_VERSION_CONDO_LIBRARY.
// Local builds fall back to the version in Cargo.toml.
fn main() {
    println!("cargo:rerun-if-env-changed=CI_VERSION_CONDO_LIBRARY");
    let version = std::env::var("CI_VERSION_CONDO_LIBRARY")
        .or_else(|_| std::env::var("CARGO_PKG_VERSION"))
        .expect("cargo sets CARGO_PKG_VERSION");
    println!("cargo:rustc-env=CONDO_VERSION={version}");
}
