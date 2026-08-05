#!/usr/bin/env bash
set -euo pipefail

# Reproduzierbare Befehlsliste für Skalierungstests.
# Standard: 500 Clients und höhere Updatefrequenzen.

SERVER_IP="192.168.153.130"
PORT="3000"
DURATION="5m"
CLIENTS="500"
RUN="scale1"
HZS=(1 5 10 20)

TECHS=(
  "http-post long-polling ''"
  "websocket websocket ''"
  "webrtc webrtc reliable-ordered"
  "webrtc webrtc unreliable-unordered"
)

for hz in "${HZS[@]}"; do
  for entry in "${TECHS[@]}"; do
    read -r tech dashboard dc <<< "$entry"

    echo "======================================================"
    echo "Scaling test | Clients: $CLIENTS | Hz: $hz | Tech: $tech | Dashboard: $dashboard | DC: $dc"
    echo

    if [[ "$tech" == "webrtc" ]]; then
      echo "SERVER:"
      echo "cd ~/ba2-live-tracking/server && ./tracking-server -technology=$tech -dashboardMode=$dashboard -dcMode=$dc -clients=$CLIENTS -hz=$hz -run=$RUN -port=$PORT"
      echo
      echo "BROWSER:"
      echo "http://${SERVER_IP}:${PORT}/dashboard?mode=webrtc\\&dcMode=${dc}"
      echo
      echo "CLIENT:"
      echo "cd ~/ba2-live-tracking/client && ./tracking-client -technology=$tech -dcMode=$dc -server=http://${SERVER_IP}:${PORT} -clients=$CLIENTS -duration=$DURATION -hz=$hz -run=$RUN"
    elif [[ "$tech" == "websocket" ]]; then
      echo "SERVER:"
      echo "cd ~/ba2-live-tracking/server && ./tracking-server -technology=websocket -dashboardMode=websocket -clients=$CLIENTS -hz=$hz -run=$RUN -port=$PORT"
      echo
      echo "BROWSER:"
      echo "http://${SERVER_IP}:${PORT}/dashboard?mode=websocket"
      echo
      echo "CLIENT:"
      echo "cd ~/ba2-live-tracking/client && ./tracking-client -technology=websocket -server=http://${SERVER_IP}:${PORT} -clients=$CLIENTS -duration=$DURATION -hz=$hz -run=$RUN"
    else
      echo "SERVER:"
      echo "cd ~/ba2-live-tracking/server && ./tracking-server -technology=http-post -dashboardMode=long-polling -clients=$CLIENTS -hz=$hz -run=$RUN -port=$PORT"
      echo
      echo "BROWSER:"
      echo "http://${SERVER_IP}:${PORT}/dashboard?mode=long-polling"
      echo
      echo "CLIENT:"
      echo "cd ~/ba2-live-tracking/client && ./tracking-client -technology=http-post -server=http://${SERVER_IP}:${PORT} -clients=$CLIENTS -duration=$DURATION -hz=$hz -run=$RUN"
    fi
    echo
  done
done
