#!/usr/bin/env bash
set -euo pipefail

# Dieses Skript ist für die Maschine gedacht, auf der das finale Repository gesammelt wird.
# Es nimmt an, dass die Server-Metriken lokal unter ./server/metrics liegen
# und die Client-Metriken entweder lokal unter ./client/metrics liegen
# oder per scp von der Client-VM kopiert werden.

BASE_DIR="${HOME}/ba2-live-tracking"
CLIENT_HOST="${CLIENT_HOST:-192.168.153.131}"
CLIENT_USER="${CLIENT_USER:-$USER}"
REMOTE_CLIENT_DIR="${REMOTE_CLIENT_DIR:-/home/$USER/ba2-live-tracking/client/metrics}"

mkdir -p \
  "$BASE_DIR/data/raw/server/ingestion" \
  "$BASE_DIR/data/raw/server/visualization" \
  "$BASE_DIR/data/raw/server/resources" \
  "$BASE_DIR/data/raw/client/load"

echo "Copying server metrics..."

cp "$BASE_DIR/server/metrics"/raw_ingestion_*.csv "$BASE_DIR/data/raw/server/ingestion/" 2>/dev/null || true
cp "$BASE_DIR/server/metrics"/raw_visualization_*.csv "$BASE_DIR/data/raw/server/visualization/" 2>/dev/null || true
cp "$BASE_DIR/server/metrics"/resources_server_process_*.csv "$BASE_DIR/data/raw/server/resources/" 2>/dev/null || true

echo "Trying to copy local client metrics..."
cp "$BASE_DIR/client/metrics"/client_load_*.csv "$BASE_DIR/data/raw/client/load/" 2>/dev/null || true

echo "Trying to copy client metrics from ${CLIENT_USER}@${CLIENT_HOST}:${REMOTE_CLIENT_DIR}"
scp "${CLIENT_USER}@${CLIENT_HOST}:${REMOTE_CLIENT_DIR}/client_load_*.csv" "$BASE_DIR/data/raw/client/load/" 2>/dev/null || true

echo "Copy complete."
echo "Raw data directories:"
echo "$BASE_DIR/data/raw/server/ingestion"
echo "$BASE_DIR/data/raw/server/visualization"
echo "$BASE_DIR/data/raw/server/resources"
echo "$BASE_DIR/data/raw/client/load"
