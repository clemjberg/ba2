# BA2 Live Tracking Benchmark

This repository contains the practical part of the bachelor's thesis on the performance analysis of communication mechanisms in a live-tracking scenario with continuous geolocation data.

## Objective

Four communication variants for a live-tracking system are compared:

| Variant | GPS Client → Server | Server → Browser Dashboard |
|---|---|---|
| HTTP-based baseline | periodic HTTP POST | HTTP Long Polling |
| WebSocket | WebSocket | WebSocket Push |
| WebRTC reliable-ordered | WebRTC DataChannel reliable + ordered | WebRTC DataChannel reliable + ordered |
| WebRTC unreliable-unordered | WebRTC DataChannel unreliable + unordered | WebRTC DataChannel unreliable + unordered |

The HTTP variant is deliberately not referred to as pure Long Polling. The ingestion path from the simulated GPS client to the server uses periodic HTTP POST because the GPS client generates the position data itself and actively sends it to the server. Long Polling is used in the HTTP-based dashboard path, where the server can deliver new position updates to the browser in a push-like manner.

## Test Environment

Two Ubuntu 22.04 VMs are used:

| Role | IP Address |
|---|---|
| Server VM | `192.168.153.130` |
| Client VM | `192.168.153.131` |
| Browser Dashboard | Host laptop |

The exact hardware and software environment is documented using the scripts in `scripts/` and stored in `docs/environment/`.

## Project Structure

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

## Creating the full project directory structure

Use the following commands to create the complete directory structure expected by the project:

```bash
mkdir -p ba2-live-tracking
cd ~/ba2-live-tracking

# Analysis, scripts, and documentation
mkdir -p analysis
mkdir -p scripts
mkdir -p docs/environment

# Server and client metric output directories
mkdir -p server/metrics
mkdir -p client/metrics

# Raw data
mkdir -p data/raw/server/ingestion
mkdir -p data/raw/server/visualization
mkdir -p data/raw/server/resources
mkdir -p data/raw/client/load

# Cleaned data
mkdir -p data/cleaned

# Result outputs
mkdir -p results/tables
mkdir -p results/plots
```


## Installation on Both VMs

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

Install Go:

```bash
cd /tmp
wget https://go.dev/dl/go1.24.4.linux-amd64.tar.gz

sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.24.4.linux-amd64.tar.gz

echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

go version
```

## Time Synchronization

On both VMs:

```bash
sudo systemctl enable --now chrony
sudo systemctl restart chrony

chronyc tracking
chronyc sources -v
timedatectl
date +"%Y-%m-%d %H:%M:%S.%N %Z"
```

For documentation:

Server VM:

```bash
scripts/collect_server_environment.sh
```

Client VM:

```bash
scripts/collect_client_environment.sh
```

The thesis should document that the system clocks were synchronized using `chrony`. Measurement runs with systematically negative latency or noticeable clock offsets are discarded and repeated.

In addition, the client estimates an offset relative to the server time via `/api/clock` and uses this offset for `t0ClientGeneratedMs`.

## Build

### Server VM

```bash
cd ~/ba2-live-tracking/server
go mod tidy
gofmt -w main.go
go build -o tracking-server main.go
```

### Client VM

```bash
cd ~/ba2-live-tracking/client
go mod tidy
gofmt -w main.go
go build -o tracking-client main.go
```

## Running the communication variants

The server and client can be started with the following commands, depending on the evaluated technology:

```bash
# HTTP POST + Long Polling baseline
./tracking-server -technology=http-post -dashboardMode=long-polling -clients=<clients> -hz=<hz> -run=<run> -port=3000
./tracking-client -technology=http-post -server=http://<server-ip>:3000 -clients=<clients> -duration=5m -hz=<hz> -run=<run> -syncClock=true

# WebSocket
./tracking-server -technology=websocket -dashboardMode=websocket -clients=<clients> -hz=<hz> -run=<run> -port=3000
./tracking-client -technology=websocket -server=http://<server-ip>:3000 -clients=<clients> -duration=5m -hz=<hz> -run=<run> -syncClock=true

# WebRTC reliable ordered DataChannel
./tracking-server -technology=webrtc -dashboardMode=webrtc -dcMode=reliable-ordered -clients=<clients> -hz=<hz> -run=<run> -port=3000
./tracking-client -technology=webrtc -dcMode=reliable-ordered -server=http://<server-ip>:3000 -clients=<clients> -duration=5m -hz=<hz> -run=<run> -syncClock=true

# WebRTC unreliable unordered DataChannel
./tracking-server -technology=webrtc -dashboardMode=webrtc -dcMode=unreliable-unordered -clients=<clients> -hz=<hz> -run=<run> -port=3000
./tracking-client -technology=webrtc -dcMode=unreliable-unordered -server=http://<server-ip>:3000 -clients=<clients> -duration=5m -hz=<hz> -run=<run> -syncClock=true
```

Replace <server-ip>, <clients>, <hz>, and <run> with the desired test configuration.

## Dashboard URLs

| Variant | Dashboard URL |
|---|---|
| HTTP POST + Long Polling | `http://192.168.153.130:3000/dashboard?mode=long-polling` |
| WebSocket | `http://192.168.153.130:3000/dashboard?mode=websocket` |
| WebRTC reliable-ordered | `http://192.168.153.130:3000/dashboard?mode=webrtc&dcMode=reliable-ordered` |
| WebRTC unreliable-unordered | `http://192.168.153.130:3000/dashboard?mode=webrtc&dcMode=unreliable-unordered` |

## Measurement Points t0 to t5

The following timestamps are stored for each individual location update:

| Timestamp | Meaning | Location |
|---|---|---|
| `t0` | GPS update generated | Client VM |
| `t1` | Server receives update | Server VM |
| `t2` | Update available for dashboard | Server VM |
| `t3` | Browser receives update | Browser |
| `t4` | DOM updated | Browser |
| `t5` | Browser paint completed | Browser |

The following metrics are calculated from these timestamps:

```text
Network ingestion        = t1 - t0
Server processing        = t2 - t1
Dashboard delivery       = t3 - t2
DOM update               = t4 - t3
Paint delay              = t5 - t4
End-to-end visualization = t5 - t0
```

## Jitter

Jitter is calculated per client:

```text
jitter_ms = abs(latency_current_for_client_i - latency_previous_for_client_i)
```

This prevents latency values from different clients from being mixed together.

## Resource Measurement

Resource measurement refers exclusively to the Go server process.

The following metrics are measured:

```text
cpu_percent_process
ram_mb_process
go_goroutines
```

The following are not included:

```text
Client VM
Browser
Host system
Virtualization overhead
Network overhead
```

## Main Test Matrix

For the main measurement:

```text
Technologies:
1. HTTP POST + Long Polling Dashboard
2. WebSocket
3. WebRTC reliable-ordered
4. WebRTC unreliable-unordered

Clients:
1
100
500

Frequency:
1 Hz

Duration:
5 minutes

Runs:
run1, run2, run3
```

This results in:

```text
4 × 3 × 3 = 36 measurement runs
```

## Procedure per Measurement Run

1. Start the server in the appropriate mode.
2. Open the corresponding browser dashboard.
3. Start the client simulator.
4. Wait 5 minutes.
5. Check the client output.
6. Stop the server with `CTRL + C`.
7. Check the CSV files.
8. Wait 5–10 seconds.
9. Start the next run.

The VMs do not need to be restarted after every run. Restarting the server process is usually sufficient.

## Raw Data

After each run, files are generated on the server and client VMs.

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

For analysis, they are collected in:

```text
data/raw/server/ingestion/
data/raw/server/visualization/
data/raw/server/resources/
data/raw/client/load/
```

## Analysis

Create the Python environment:

```bash
cd ~/ba2-live-tracking
python3 -m venv .venv
source .venv/bin/activate
pip install pandas numpy matplotlib
```

Run the analysis:

```bash
python analysis/analyze_ingestion.py
python analysis/analyze_visualization.py
python analysis/analyze_resources.py
python analysis/analyze_client_load.py
python analysis/generate_tables.py
python analysis/generate_plots.py
```

## Cleaning Rules

By default, the scripts use the following rules:

1. The first 10 seconds of each run are removed as warm-up.
2. The final 5 seconds are removed to reduce shutdown effects.
3. Negative latencies are treated as an indication of clock synchronization issues.
4. Runs with systematically negative latency should be discarded and repeated.
5. For visualization latency, only messages with complete `t0` to `t5` timestamps are evaluated.
6. For WebRTC unreliable-unordered, message loss is reported separately as the delivery ratio.
7. Jitter is calculated per client.

## Result Files

The final tables and plots are located under:

```text
results/tables/
results/plots/
```

Typical outputs:

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

## Scaling Tests

In addition to the main test matrix, scaling tests can be performed:

```text
500 Clients × 1 Hz
500 Clients × 5 Hz
500 Clients × 10 Hz
500 Clients × 20 Hz
```

Optionally, the number of clients can also be increased:

```text
1000 Clients
2000 Clients
```

The following termination criteria apply:

```text
- Errors increase sharply
- Throughput remains significantly below the target
- Server process crashes
- Connections cannot be established
- CPU remains continuously saturated
- Latencies increase significantly
```

## Limitations

The HTTP-based variant uses HTTP POST for the ingestion path and HTTP Long Polling for dashboard updates. It is therefore not a pure Long Polling implementation. This is methodologically intentional because GPS clients generate position data themselves and actively send it to the server.

Resource measurement considers only the Go server process. The resource usage of the client VM, browser, host system, hypervisor, and network is not evaluated as part of this metric.

The browser paint measurement uses two `requestAnimationFrame` calls to approximate the visible paint timestamp more closely. This method is a practical approximation and not a perfect measurement of the user's perceived display time.

With WebRTC unreliable-unordered, message loss may occur. This is part of the selected DataChannel semantics and is reported separately via the delivery ratio.
