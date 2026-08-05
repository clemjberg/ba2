#!/usr/bin/env python3
from __future__ import annotations

import re
from pathlib import Path
import pandas as pd
import numpy as np

BASE_DIR = Path.home() / "ba2-live-tracking"

def ensure_dirs() -> None:
    for path in [
        BASE_DIR / "data" / "cleaned",
        BASE_DIR / "results" / "tables",
        BASE_DIR / "results" / "plots",
    ]:
        path.mkdir(parents=True, exist_ok=True)

def parse_metadata_from_name(path: Path) -> dict:
    name = path.name
    # examples:
    # raw_ingestion_websocket_100clients_1hz_run1.csv
    # raw_visualization_webrtc-reliable-ordered_500clients_10hz_scale1.csv
    # resources_server_process_http-post-long-polling_1clients_1hz_run1.csv
    # client_load_websocket_100clients_1hz_run1.csv
    m = re.search(r"_(?P<label>.+)_(?P<clients>\d+)clients_(?P<hz>\d+)hz_(?P<run>[^.]+)\.csv$", name)
    if not m:
        return {"file": name}
    return {
        "file": name,
        "test_label": m.group("label"),
        "scenario_clients_from_file": int(m.group("clients")),
        "hz_from_file": int(m.group("hz")),
        "run_id_from_file": m.group("run"),
    }

def read_many(pattern: str) -> pd.DataFrame:
    files = sorted(BASE_DIR.glob(pattern))
    frames = []
    for file in files:
        try:
            df = pd.read_csv(file)
        except pd.errors.EmptyDataError:
            continue
        if df.empty:
            continue
        meta = parse_metadata_from_name(file)
        for key, value in meta.items():
            df[key] = value
        frames.append(df)
    if not frames:
        return pd.DataFrame()
    return pd.concat(frames, ignore_index=True)

def add_elapsed_seconds(df: pd.DataFrame, timestamp_col: str) -> pd.DataFrame:
    if df.empty or timestamp_col not in df.columns:
        return df
    df = df.copy()
    group_cols = [c for c in ["test_label", "scenario_clients", "hz", "run_id"] if c in df.columns]
    if not group_cols:
        group_cols = [c for c in ["file"] if c in df.columns]
    df["_run_start_ms"] = df.groupby(group_cols)[timestamp_col].transform("min")
    df["elapsed_seconds"] = (df[timestamp_col] - df["_run_start_ms"]) / 1000.0
    return df

def trim_warmup_shutdown(df: pd.DataFrame, timestamp_col: str, warmup_s: int = 10, shutdown_s: int = 5) -> pd.DataFrame:
    if df.empty or timestamp_col not in df.columns:
        return df
    df = add_elapsed_seconds(df, timestamp_col)
    group_cols = [c for c in ["test_label", "scenario_clients", "hz", "run_id"] if c in df.columns]
    if not group_cols:
        group_cols = [c for c in ["file"] if c in df.columns]
    max_elapsed = df.groupby(group_cols)["elapsed_seconds"].transform("max")
    return df[(df["elapsed_seconds"] >= warmup_s) & (df["elapsed_seconds"] <= (max_elapsed - shutdown_s))].copy()

def summarize(df: pd.DataFrame, metric: str, group_cols: list[str]) -> pd.DataFrame:
    if df.empty or metric not in df.columns:
        return pd.DataFrame()
    d = df.dropna(subset=[metric]).copy()
    if d.empty:
        return pd.DataFrame()
    grouped = d.groupby(group_cols, dropna=False)[metric]
    out = grouped.agg(
        count="count",
        mean="mean",
        median="median",
        std="std",
        min="min",
        max="max",
    ).reset_index()
    out["p95"] = grouped.quantile(0.95).values
    out["p99"] = grouped.quantile(0.99).values
    q75 = grouped.quantile(0.75).values
    q25 = grouped.quantile(0.25).values
    out["iqr"] = q75 - q25
    out.insert(len(group_cols), "metric", metric)
    return out

def save(df: pd.DataFrame, relative_path: str) -> None:
    target = BASE_DIR / relative_path
    target.parent.mkdir(parents=True, exist_ok=True)
    df.to_csv(target, index=False)
    print(f"Wrote {target}")

def load_table(name: str) -> pd.DataFrame:
    path = BASE_DIR / "results" / "tables" / name
    if path.exists():
        return pd.read_csv(path)
    return pd.DataFrame()

def pivot_metric(summary: pd.DataFrame, metric: str, value_cols: list[str]) -> pd.DataFrame:
    if summary.empty:
        return pd.DataFrame()
    d = summary[summary["metric"] == metric].copy()
    if d.empty:
        return pd.DataFrame()
    cols = [c for c in ["test_label", "scenario_clients", "hz", "run_id"] + value_cols if c in d.columns]
    return d[cols].sort_values([c for c in ["scenario_clients", "test_label", "run_id"] if c in cols])

def main() -> None:
    ensure_dirs()

    ingestion = load_table("ingestion_summary.csv")
    visualization = load_table("visualization_summary.csv")
    resources = load_table("resources_summary.csv")
    client_load = load_table("client_load_summary.csv")

    table2 = pivot_metric(ingestion, "latency_ms", ["count", "mean", "median", "std", "iqr", "p95", "p99", "min", "max"])
    if not table2.empty:
        save(table2, "results/tables/table_2_latency.csv")

    table3 = pivot_metric(ingestion, "jitter_ms", ["count", "mean", "median", "std", "iqr", "p95", "p99", "min", "max"])
    if not table3.empty:
        save(table3, "results/tables/table_3_jitter.csv")

    if not client_load.empty:
        cols = [c for c in ["test_label", "scenario_clients", "hz", "run_id", "sent_messages_total", "expected_messages_5m", "delivery_ratio_client_sent_vs_expected", "errors_total"] if c in client_load.columns]
        table4 = client_load[cols].copy()
        save(table4, "results/tables/table_4_throughput.csv")

    if not resources.empty:
        cpu = pivot_metric(resources, "cpu_percent_process", ["mean", "median", "p95", "max"])
        ram = pivot_metric(resources, "ram_mb_process", ["mean", "median", "p95", "max"])
        cpu["resource_metric"] = "cpu_percent_process" if not cpu.empty else None
        ram["resource_metric"] = "ram_mb_process" if not ram.empty else None
        table5 = pd.concat([cpu, ram], ignore_index=True)
        if not table5.empty:
            save(table5, "results/tables/table_5_cpu_ram.csv")

    table6 = pivot_metric(visualization, "end_to_end_visualization_ms", ["count", "mean", "median", "std", "iqr", "p95", "p99", "min", "max"])
    if not table6.empty:
        save(table6, "results/tables/table_6_visualization_e2e.csv")

    if not visualization.empty:
        breakdown_metrics = [
            "network_ingestion_ms",
            "server_processing_ms",
            "dashboard_delivery_ms",
            "dom_update_ms",
            "paint_delay_ms",
        ]
        parts = []
        for metric in breakdown_metrics:
            p = pivot_metric(visualization, metric, ["mean", "median", "p95"])
            if not p.empty:
                p["delay_part"] = metric
                parts.append(p)
        if parts:
            save(pd.concat(parts, ignore_index=True), "results/tables/table_7_delay_breakdown.csv")

    # Delivery ratio can later be extended with server-received vs client-sent matching.
    if not client_load.empty:
        cols = [c for c in ["test_label", "scenario_clients", "hz", "run_id", "delivery_ratio_client_sent_vs_expected", "errors_total"] if c in client_load.columns]
        save(client_load[cols], "results/tables/table_8_delivery_ratio.csv")

if __name__ == "__main__":
    main()
