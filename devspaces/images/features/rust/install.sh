#!/usr/bin/env bash
# Rust stable via rustup. The toolchain is installed into /opt/rust at build time and
# seeded into ~/.rustup / ~/.cargo on first boot, so `rustup toolchain add nightly`
# and `cargo install` persist on the volume.
set -Eeuo pipefail; . /opt/workspace/lib.sh
apt_install libssl-dev clang lld
export RUSTUP_HOME=/opt/rust/rustup CARGO_HOME=/opt/rust/cargo
if [ ! -x "$CARGO_HOME/bin/cargo" ]; then
  fetch https://sh.rustup.rs /tmp/rustup.sh
  sh /tmp/rustup.sh -y --no-modify-path --profile default --default-toolchain stable \
     -c rust-analyzer -c rust-src -c clippy -c rustfmt
  rm -f /tmp/rustup.sh
fi
chmod -R a+rX /opt/rust
seed_dir /opt/rust/rustup "/home/dev/.rustup"
seed_dir /opt/rust/cargo  "/home/dev/.cargo"
profile_env rust \
  'export RUSTUP_HOME="$HOME/.rustup" CARGO_HOME="$HOME/.cargo"' \
  'export PATH="$HOME/.cargo/bin:$PATH"'
