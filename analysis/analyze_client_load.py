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

def main() -> None:
    ensure_dirs()
    df = read_many("data/raw/client/load/client_load_*.csv")
    if df.empty:
        print("No client load files found.")
        return

    numeric_cols = [
        "timestamp_ms", "scenario_clients", "hz", "sent_messages_total",
        "sent_messages_per_second", "errors_total", "errors_per_second", "clock_offset_ms"
    ]
    for col in numeric_cols:
        if col in df.columns:
            df[col] = pd.to_numeric(df[col], errors="coerce")

    cleaned = trim_warmup_shutdown(df, "timestamp_ms")
    save(cleaned, "data/cleaned/client_load_cleaned.csv")

    group_cols = ["test_label", "technology", "data_channel_mode", "scenario_clients", "hz", "run_id"]
    available_group_cols = [c for c in group_cols if c in cleaned.columns]

    final_rows = df.sort_values("timestamp_ms").groupby(["file"], dropna=False).tail(1).copy()
    expected_duration_s = 300
    final_rows["expected_messages_5m"] = final_rows["scenario_clients"] * final_rows["hz"] * expected_duration_s
    final_rows["delivery_ratio_client_sent_vs_expected"] = final_rows["sent_messages_total"] / final_rows["expected_messages_5m"]

    save(final_rows, "results/tables/client_load_summary.csv")

    summaries = []
    for metric in ["sent_messages_per_second", "errors_per_second", "clock_offset_ms"]:
        s = summarize(cleaned, metric, available_group_cols)
        if not s.empty:
            summaries.append(s)
    if summaries:
        save(pd.concat(summaries, ignore_index=True), "results/tables/client_load_timeseries_summary.csv")

if __name__ == "__main__":
    main()
