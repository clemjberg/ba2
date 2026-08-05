#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="${HOME}/ba2-live-tracking"

mkdir -p \
  "$BASE_DIR/data/raw/server/ingestion" \
  "$BASE_DIR/data/raw/server/visualization" \
  "$BASE_DIR/data/raw/server/resources" \
  "$BASE_DIR/data/raw/client/load" \
  "$BASE_DIR/data/cleaned" \
  "$BASE_DIR/results/tables" \
  "$BASE_DIR/results/plots" \
  "$BASE_DIR/docs/environment" \
  "$BASE_DIR/server/metrics" \
  "$BASE_DIR/client/metrics"

echo "Result directories prepared under $BASE_DIR"
