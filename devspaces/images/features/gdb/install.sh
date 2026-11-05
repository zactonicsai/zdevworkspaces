#!/usr/bin/env bash
# Debuggers and tracers. ptrace of your own processes needs no extra capability;
# attaching across users (sudo gdb -p) needs SYS_PTRACE (lab profiles).
set -Eeuo pipefail; . /opt/workspace/lib.sh
apt_install gdb gdbserver lldb strace ltrace valgrind linux-tools-common 2>/dev/null || apt_install gdb gdbserver lldb strace ltrace valgrind
