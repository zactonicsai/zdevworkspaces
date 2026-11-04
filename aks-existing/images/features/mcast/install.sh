#!/usr/bin/env bash
# Multicast test kit: traffic generators, an IGMP-aware routing daemon, capture tools,
# and `mcast-check` which verifies the pod can join a group and see its own/others' datagrams.
set -Eeuo pipefail; . /opt/workspace/lib.sh
apt_install iperf iperf3 smcroute tcpdump socat iproute2 python3
install -m 755 "$FEATURE_DIR/mcast-check" /usr/local/bin/mcast-check
