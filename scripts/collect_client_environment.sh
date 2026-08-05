#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="${HOME}/ba2-live-tracking"
OUT_DIR="${BASE_DIR}/docs/environment"

mkdir -p "$OUT_DIR"

echo "Collecting client environment..."

hostname > "$OUT_DIR/client_hostname.txt"
date +"%Y-%m-%d %H:%M:%S.%N %Z" > "$OUT_DIR/client_date.txt"
timedatectl > "$OUT_DIR/client_timedatectl.txt" || true
chronyc tracking > "$OUT_DIR/client_chrony_tracking.txt" || true
chronyc sources -v > "$OUT_DIR/client_chrony_sources.txt" || true
lscpu > "$OUT_DIR/client_lscpu.txt"
free -h > "$OUT_DIR/client_memory.txt"
lsblk > "$OUT_DIR/client_disks.txt"
uname -a > "$OUT_DIR/client_uname.txt"
ip addr > "$OUT_DIR/client_ip_addr.txt"
go version > "$OUT_DIR/client_go_version.txt" || true

echo "Client environment written to $OUT_DIR"
