# BA2 Live Tracking Benchmark

Dieses Repository enthält den praktischen Teil der Bachelorarbeit zur Performanceanalyse von Kommunikationsmechanismen in einem Live-Tracking-Szenario mit kontinuierlichen Geolokationsdaten.

## Ziel

Verglichen werden vier Kommunikationsvarianten für ein Live-Tracking-System:

| Variante | GPS-Client → Server | Server → Browser-Dashboard |
|---|---|---|
| HTTP-basierte Baseline | periodischer HTTP POST | HTTP Long Polling |
| WebSocket | WebSocket | WebSocket Push |
| WebRTC reliable-ordered | WebRTC DataChannel reliable + ordered | WebRTC DataChannel reliable + ordered |
| WebRTC unreliable-unordered | WebRTC DataChannel unreliable + unordered | WebRTC DataChannel unreliable + unordered |

Die HTTP-Variante wird bewusst nicht als reines Long Polling bezeichnet. Der Ingestion-Pfad vom simulierten GPS-Client zum Server verwendet periodisches HTTP POST, weil der GPS-Client die Positionsdaten selbst erzeugt und aktiv an den Server sendet. Long Polling wird im HTTP-basierten Dashboard-Pfad eingesetzt, wo der Server neue Positionsupdates push-ähnlich an den Browser liefern kann.

## Testumgebung

Verwendet werden zwei Ubuntu-22.04-VMs:

| Rolle | IP-Adresse |
|---|---|
| Server-VM | `192.168.153.130` |
| Client-VM | `192.168.153.131` |
| Browser-Dashboard | Host-Laptop |

Die genaue Hardware- und Softwareumgebung wird mit den Skripten in `scripts/` dokumentiert und in `docs/environment/` gespeichert.

## Projektstruktur

```text
ba2-live-tracking/
├── README.md
├── server/
│   ├── main.go
│   ├── go.mod
│   └── go.sum
├── client/
│   ├── main.go
│   ├── go.mod
│   └── go.sum
├── analysis/
│   ├── analyze_ingestion.py
│   ├── analyze_visualization.py
│   ├── analyze_resources.py
│   ├── analyze_client_load.py
│   ├── generate_tables.py
│   └── generate_plots.py
├── scripts/
│   ├── collect_server_environment.sh
│   ├── collect_client_environment.sh
│   ├── prepare_result_dirs.sh
│   ├── clean_metrics.sh
│   ├── copy_results_from_vms.sh
│   ├── run_main_matrix.sh
│   └── run_scaling_tests.sh
├── docs/
│   ├── experiment_log.md
│   └── environment/
├── data/
│   ├── raw/
│   └── cleaned/
└── results/
    ├── tables/
    └── plots/
```

## Installation auf beiden VMs

```bash
sudo apt update
sudo apt upgrade -y

sudo apt install -y \
  curl wget git build-essential \
  chrony \
  python3 python3-pip python3-venv \
  sysstat \
  net-tools
```

Go installieren:

```bash
cd /tmp
wget https://go.dev/dl/go1.24.4.linux-amd64.tar.gz

sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.24.4.linux-amd64.tar.gz

echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

go version
```

## Zeitsynchronisation

Auf beiden VMs:

```bash
sudo systemctl enable --now chrony
sudo systemctl restart chrony

chronyc tracking
chronyc sources -v
timedatectl
date +"%Y-%m-%d %H:%M:%S.%N %Z"
```

Für die Dokumentation:

Server-VM:

```bash
scripts/collect_server_environment.sh
```

Client-VM:

```bash
scripts/collect_client_environment.sh
```

In der Arbeit sollte dokumentiert werden, dass die Systemzeiten mit `chrony` synchronisiert wurden. Messläufe mit systematisch negativer Latenz oder auffälligem Zeitversatz werden verworfen und wiederholt.

Zusätzlich schätzt der Client über `/api/clock` einen Offset zur Serverzeit und verwendet diesen Offset für `t0ClientGeneratedMs`.

## Build

### Server-VM

```bash
cd ~/ba2-live-tracking/server
go mod tidy
gofmt -w main.go
go build -o tracking-server main.go
```

### Client-VM

```bash
cd ~/ba2-live-tracking/client
go mod tidy
gofmt -w main.go
go build -o tracking-client main.go
```

## Dashboard-URLs

| Variante | Dashboard-URL |
|---|---|
| HTTP POST + Long Polling | `http://192.168.153.130:3000/dashboard?mode=long-polling` |
| WebSocket | `http://192.168.153.130:3000/dashboard?mode=websocket` |
| WebRTC reliable-ordered | `http://192.168.153.130:3000/dashboard?mode=webrtc&dcMode=reliable-ordered` |
| WebRTC unreliable-unordered | `http://192.168.153.130:3000/dashboard?mode=webrtc&dcMode=unreliable-unordered` |

## Messpunkte t0 bis t5

Für jedes einzelne Locationupdate werden folgende Zeitpunkte gespeichert:

| Zeitpunkt | Bedeutung | Ort |
|---|---|---|
| `t0` | GPS-Update erzeugt | Client-VM |
| `t1` | Server empfängt Update | Server-VM |
| `t2` | Update für Dashboard verfügbar | Server-VM |
| `t3` | Browser empfängt Update | Browser |
| `t4` | DOM wurde aktualisiert | Browser |
| `t5` | Browser Paint abgeschlossen | Browser |

Daraus werden berechnet:

```text
Network ingestion        = t1 - t0
Server processing        = t2 - t1
Dashboard delivery       = t3 - t2
DOM update               = t4 - t3
Paint delay              = t5 - t4
End-to-end visualization = t5 - t0
```

## Jitter

Jitter wird clientbezogen berechnet:

```text
jitter_ms = abs(latency_current_for_client_i - latency_previous_for_client_i)
```

Damit werden keine Latenzwerte unterschiedlicher Clients miteinander vermischt.

## Ressourcenmessung

Die Ressourcenmessung bezieht sich ausschließlich auf den Go-Serverprozess.

Gemessen werden:

```text
cpu_percent_process
ram_mb_process
go_goroutines
```

Nicht enthalten sind:

```text
Client-VM
Browser
Host-System
Virtualisierungs-Overhead
Netzwerk-Overhead
```

## Haupt-Testmatrix

Für die Hauptmessung:

```text
Technologien:
1. HTTP POST + Long Polling Dashboard
2. WebSocket
3. WebRTC reliable-ordered
4. WebRTC unreliable-unordered

Clients:
1
100
500

Frequenz:
1 Hz

Dauer:
5 Minuten

Runs:
run1, run2, run3
```

Das ergibt:

```text
4 × 3 × 3 = 36 Messläufe
```

## Ablauf pro Messlauf

1. Server mit passendem Modus starten.
2. Passendes Browser-Dashboard öffnen.
3. Client-Simulator starten.
4. 5 Minuten warten.
5. Client-Ausgabe prüfen.
6. Server mit `CTRL + C` stoppen.
7. CSV-Dateien prüfen.
8. 5–10 Sekunden warten.
9. Nächsten Lauf starten.

Die VMs müssen nicht nach jedem Lauf neugestartet werden. Ein Neustart des Serverprozesses reicht normalerweise aus.

## Rohdaten

Nach jedem Lauf entstehen Dateien auf Server- und Client-VM.

Server:

```text
server/metrics/raw_ingestion_*.csv
server/metrics/raw_visualization_*.csv
server/metrics/resources_server_process_*.csv
```

Client:

```text
client/metrics/client_load_*.csv
```

Für die Auswertung werden sie gesammelt nach:

```text
data/raw/server/ingestion/
data/raw/server/visualization/
data/raw/server/resources/
data/raw/client/load/
```

## Analyse

Python-Umgebung erstellen:

```bash
cd ~/ba2-live-tracking
python3 -m venv .venv
source .venv/bin/activate
pip install pandas numpy matplotlib
```

Analyse ausführen:

```bash
python analysis/analyze_ingestion.py
python analysis/analyze_visualization.py
python analysis/analyze_resources.py
python analysis/analyze_client_load.py
python analysis/generate_tables.py
python analysis/generate_plots.py
```

## Cleaning-Regeln

Die Skripte verwenden standardmäßig folgende Regeln:

1. Die ersten 10 Sekunden jedes Runs werden als Warm-up entfernt.
2. Die letzten 5 Sekunden werden entfernt, um Shutdown-Effekte zu reduzieren.
3. Negative Latenzen werden als Hinweis auf Clock-Probleme behandelt.
4. Runs mit systematisch negativer Latenz sollten verworfen und wiederholt werden.
5. Für Visualisierungslatenzen werden nur Nachrichten mit vollständigem `t0` bis `t5` ausgewertet.
6. Bei WebRTC unreliable-unordered wird Message Loss separat als Delivery Ratio ausgewiesen.
7. Jitter wird pro Client berechnet.

## Ergebnisdateien

Die finalen Tabellen und Diagramme liegen unter:

```text
results/tables/
results/plots/
```

Typische Ausgaben:

```text
results/tables/ingestion_summary.csv
results/tables/visualization_summary.csv
results/tables/resources_summary.csv
results/tables/client_load_summary.csv
results/tables/table_2_latency.csv
results/tables/table_3_jitter.csv
results/tables/table_4_throughput.csv
results/tables/table_5_cpu_ram.csv
results/tables/table_6_visualization_e2e.csv
results/tables/table_7_delay_breakdown.csv
results/tables/table_8_delivery_ratio.csv
```

## Skalierungstests

Zusätzlich zur Hauptmatrix können Skalierungstests durchgeführt werden:

```text
500 Clients × 1 Hz
500 Clients × 5 Hz
500 Clients × 10 Hz
500 Clients × 20 Hz
```

Optional kann auch die Clientzahl erhöht werden:

```text
1000 Clients
2000 Clients
```

Dabei gelten Abbruchkriterien:

```text
- Errors steigen stark
- Throughput bleibt deutlich unter Soll
- Serverprozess stürzt ab
- Verbindungen können nicht aufgebaut werden
- CPU ist dauerhaft gesättigt
- Latenzen steigen massiv
```

## Limitationen

Die HTTP-basierte Variante verwendet HTTP POST für den Ingestion-Pfad und HTTP Long Polling für die Dashboard-Aktualisierung. Sie ist daher keine reine Long-Polling-Implementierung. Das ist methodisch beabsichtigt, weil GPS-Clients Positionsdaten selbst erzeugen und aktiv an den Server senden.

Die Ressourcenmessung betrachtet ausschließlich den Go-Serverprozess. Der Ressourcenbedarf der Client-VM, des Browsers, des Host-Systems, des Hypervisors und des Netzwerks wird nicht als Teil dieser Metrik ausgewertet.

Die Browser-Paint-Messung verwendet zwei `requestAnimationFrame`-Aufrufe, um näher an den sichtbaren Paint-Zeitpunkt zu kommen. Diese Methode ist eine praktische Annäherung und keine perfekte Messung des wahrgenommenen Benutzerzeitpunkts.

Bei WebRTC unreliable-unordered kann Message Loss auftreten. Dies ist Teil der gewählten DataChannel-Semantik und wird separat über Delivery Ratio ausgewiesen.
