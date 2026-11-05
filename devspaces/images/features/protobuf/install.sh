#!/usr/bin/env bash
set -Eeuo pipefail; . /opt/workspace/lib.sh
apt_install protobuf-compiler protobuf-compiler-grpc libprotobuf-dev libgrpc++-dev
