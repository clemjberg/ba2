#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="${HOME}/ba2-live-tracking"
OUT_DIR="${BASE_DIR}/docs/environment"

mkdir -p "$OUT_DIR"

echo "Collecting server environment..."

hostname > "$OUT_DIR/server_hostname.txt"
date +"%Y-%m-%d %H:%M:%S.%N %Z" > "$OUT_DIR/server_date.txt"
timedatectl > "$OUT_DIR/server_timedatectl.txt" || true
chronyc tracking > "$OUT_DIR/server_chrony_tracking.txt" || true
chronyc sources -v > "$OUT_DIR/server_chrony_sources.txt" || true
lscpu > "$OUT_DIR/server_lscpu.txt"
free -h > "$OUT_DIR/server_memory.txt"
lsblk > "$OUT_DIR/server_disks.txt"
uname -a > "$OUT_DIR/server_uname.txt"
ip addr > "$OUT_DIR/server_ip_addr.txt"
go version > "$OUT_DIR/server_go_version.txt" || true

echo "Server environment written to $OUT_DIR"
