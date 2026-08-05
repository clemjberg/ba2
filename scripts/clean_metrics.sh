#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="${HOME}/ba2-live-tracking"

echo "This script removes generated metrics and result files."
echo "Press CTRL+C within 5 seconds to cancel."
sleep 5

rm -f "$BASE_DIR/server/metrics/"*.csv 2>/dev/null || true
rm -f "$BASE_DIR/client/metrics/"*.csv 2>/dev/null || true
rm -f "$BASE_DIR/data/raw/server/ingestion/"*.csv 2>/dev/null || true
rm -f "$BASE_DIR/data/raw/server/visualization/"*.csv 2>/dev/null || true
rm -f "$BASE_DIR/data/raw/server/resources/"*.csv 2>/dev/null || true
rm -f "$BASE_DIR/data/raw/client/load/"*.csv 2>/dev/null || true
rm -f "$BASE_DIR/data/cleaned/"*.csv 2>/dev/null || true
rm -f "$BASE_DIR/results/tables/"*.csv 2>/dev/null || true
rm -f "$BASE_DIR/results/plots/"*.png 2>/dev/null || true

echo "Metrics and generated result files removed."
