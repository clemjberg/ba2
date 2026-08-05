#!/usr/bin/env bash
set -euo pipefail

# Dieses Skript dient als reproduzierbare Befehlsliste.
# Praktisch müssen Server, Browser-Dashboard und Client koordiniert werden.
# Deshalb gibt das Skript die Befehle aus, statt alles automatisch parallel zu starten.

SERVER_IP="192.168.153.130"
PORT="3000"
DURATION="5m"
HZ="1"

TECHS=(
  "http-post long-polling ''"
  "websocket websocket ''"
  "webrtc webrtc reliable-ordered"
  "webrtc webrtc unreliable-unordered"
)

CLIENTS=(1 100 500)
RUNS=(run1 run2 run3)

for run in "${RUNS[@]}"; do
  for clients in "${CLIENTS[@]}"; do
    for entry in "${TECHS[@]}"; do
      read -r tech dashboard dc <<< "$entry"

      echo "======================================================"
      echo "Run: $run | Clients: $clients | Hz: $HZ | Tech: $tech | Dashboard: $dashboard | DC: $dc"
      echo

      if [[ "$tech" == "webrtc" ]]; then
        echo "SERVER:"
        echo "cd ~/ba2-live-tracking/server && ./tracking-server -technology=$tech -dashboardMode=$dashboard -dcMode=$dc -clients=$clients -hz=$HZ -run=$run -port=$PORT"
        echo
        echo "BROWSER:"
        echo "http://${SERVER_IP}:${PORT}/dashboard?mode=webrtc\\&dcMode=${dc}"
        echo
        echo "CLIENT:"
        echo "cd ~/ba2-live-tracking/client && ./tracking-client -technology=$tech -dcMode=$dc -server=http://${SERVER_IP}:${PORT} -clients=$clients -duration=$DURATION -hz=$HZ -run=$run"
      elif [[ "$tech" == "websocket" ]]; then
        echo "SERVER:"
        echo "cd ~/ba2-live-tracking/server && ./tracking-server -technology=websocket -dashboardMode=websocket -clients=$clients -hz=$HZ -run=$run -port=$PORT"
        echo
        echo "BROWSER:"
        echo "http://${SERVER_IP}:${PORT}/dashboard?mode=websocket"
        echo
        echo "CLIENT:"
        echo "cd ~/ba2-live-tracking/client && ./tracking-client -technology=websocket -server=http://${SERVER_IP}:${PORT} -clients=$clients -duration=$DURATION -hz=$HZ -run=$run"
      else
        echo "SERVER:"
        echo "cd ~/ba2-live-tracking/server && ./tracking-server -technology=http-post -dashboardMode=long-polling -clients=$clients -hz=$HZ -run=$run -port=$PORT"
        echo
        echo "BROWSER:"
        echo "http://${SERVER_IP}:${PORT}/dashboard?mode=long-polling"
        echo
        echo "CLIENT:"
        echo "cd ~/ba2-live-tracking/client && ./tracking-client -technology=http-post -server=http://${SERVER_IP}:${PORT} -clients=$clients -duration=$DURATION -hz=$HZ -run=$run"
      fi
      echo
    done
  done
done
