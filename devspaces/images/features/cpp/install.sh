#!/usr/bin/env bash
# C/C++: GCC + Clang/LLVM toolchain, CMake, Ninja, GDB, Valgrind, clangd for the editor.
set -Eeuo pipefail; . /opt/workspace/lib.sh
apt_install gcc g++ clang lld lldb clangd clang-format clang-tidy \
  cmake ninja-build ccache gdb valgrind cppcheck bear libc6-dbg \
  libtool autoconf automake
update-alternatives --install /usr/bin/clangd clangd "$(ls /usr/bin/clangd-* | sort -V | tail -1)" 100 2>/dev/null || true
profile_env cpp \
  'export CCACHE_DIR="$HOME/.cache/ccache" CMAKE_GENERATOR=Ninja' \
  'export CMAKE_C_COMPILER_LAUNCHER=ccache CMAKE_CXX_COMPILER_LAUNCHER=ccache'
