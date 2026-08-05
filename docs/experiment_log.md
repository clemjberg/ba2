# Experiment Log

Dieses Log wird während der Messungen manuell ergänzt.

## Vorlage

```text
Run ID:
Date:
Technology:
DataChannel Mode:
Dashboard Mode:
Clients:
Hz:
Duration:
Server command:
Dashboard URL:
Client command:
Expected messages:
Sent messages:
Client errors:
Server raw_ingestion file:
Server raw_visualization file:
Server resources file:
Client load file:
Valid run: yes/no
Notes:
```

---

## Beispiel

```text
Run ID: run1
Date: 2026-07-28
Technology: websocket
DataChannel Mode:
Dashboard Mode: websocket
Clients: 100
Hz: 1
Duration: 5m
Server command: ./tracking-server -technology=websocket -dashboardMode=websocket -clients=100 -hz=1 -run=run1 -port=3000
Dashboard URL: http://192.168.153.130:3000/dashboard?mode=websocket
Client command: ./tracking-client -technology=websocket -server=http://192.168.153.130:3000 -clients=100 -duration=5m -hz=1 -run=run1
Expected messages: 30000
Sent messages: 30000
Client errors: 0
Server raw_ingestion file: raw_ingestion_websocket_100clients_1hz_run1.csv
Server raw_visualization file: raw_visualization_websocket_100clients_1hz_run1.csv
Server resources file: resources_server_process_websocket_100clients_1hz_run1.csv
Client load file: client_load_websocket_100clients_1hz_run1.csv
Valid run: yes
Notes: no issues
```
