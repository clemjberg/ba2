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

import matplotlib.pyplot as plt

def save_plot(fig, name: str) -> None:
    out = BASE_DIR / "results" / "plots" / name
    out.parent.mkdir(parents=True, exist_ok=True)
    fig.tight_layout()
    fig.savefig(out, dpi=180)
    plt.close(fig)
    print(f"Wrote {out}")

def plot_bar(df: pd.DataFrame, title: str, ylabel: str, filename: str) -> None:
    if df.empty:
        return
    fig, ax = plt.subplots(figsize=(10, 6))
    df.plot(kind="bar", x="label", y="value", ax=ax, legend=False)
    ax.set_title(title)
    ax.set_xlabel("")
    ax.set_ylabel(ylabel)
    ax.tick_params(axis="x", rotation=45)
    save_plot(fig, filename)

def main() -> None:
    ensure_dirs()

    ingestion_path = BASE_DIR / "results" / "tables" / "ingestion_summary.csv"
    visualization_path = BASE_DIR / "results" / "tables" / "visualization_summary.csv"
    resources_path = BASE_DIR / "results" / "tables" / "resources_summary.csv"

    if ingestion_path.exists():
        ingestion = pd.read_csv(ingestion_path)
        for metric, filename, ylabel, title in [
            ("latency_ms", "latency_median_by_technology.png", "Median latency (ms)", "Median ingestion latency"),
            ("jitter_ms", "jitter_median_by_technology.png", "Median jitter (ms)", "Median client-based jitter"),
        ]:
            d = ingestion[(ingestion["metric"] == metric) & (ingestion["run_id"] == "run1")].copy()
            if not d.empty:
                d["label"] = d["test_label"].astype(str) + " / " + d["scenario_clients"].astype(str) + " clients"
                d["value"] = d["median"]
                plot_bar(d[["label", "value"]], title, ylabel, filename)

        d = ingestion[(ingestion["metric"] == "latency_ms") & (ingestion["run_id"] == "run1")].copy()
        if not d.empty:
            d["label"] = d["test_label"].astype(str) + " / " + d["scenario_clients"].astype(str) + " clients"
            d["value"] = d["p95"]
            plot_bar(d[["label", "value"]], "P95 ingestion latency", "P95 latency (ms)", "latency_p95_by_technology.png")

    if visualization_path.exists():
        vis = pd.read_csv(visualization_path)
        d = vis[(vis["metric"] == "end_to_end_visualization_ms") & (vis["run_id"] == "run1")].copy()
        if not d.empty:
            d["label"] = d["test_label"].astype(str) + " / " + d["scenario_clients"].astype(str) + " clients"
            d["value"] = d["median"]
            plot_bar(d[["label", "value"]], "Median end-to-end visualization latency", "Median latency (ms)", "visualization_e2e_median_by_technology.png")

        d = vis[(vis["metric"] == "dashboard_delivery_ms") & (vis["run_id"] == "run1")].copy()
        if not d.empty:
            d["label"] = d["test_label"].astype(str) + " / " + d["scenario_clients"].astype(str) + " clients"
            d["value"] = d["median"]
            plot_bar(d[["label", "value"]], "Median dashboard delivery delay", "Median delay (ms)", "dashboard_delivery_breakdown.png")

    if resources_path.exists():
        resources = pd.read_csv(resources_path)
        for metric, filename, ylabel, title in [
            ("cpu_percent_process", "cpu_mean_by_technology.png", "Mean CPU (%)", "Mean server process CPU"),
            ("ram_mb_process", "ram_max_by_technology.png", "Max RAM (MB)", "Max server process RAM"),
        ]:
            d = resources[(resources["metric"] == metric) & (resources["run_id"] == "run1")].copy()
            if not d.empty:
                d["label"] = d["test_label"].astype(str) + " / " + d["scenario_clients"].astype(str) + " clients"
                d["value"] = d["mean"] if metric == "cpu_percent_process" else d["max"]
                plot_bar(d[["label", "value"]], title, ylabel, filename)

if __name__ == "__main__":
    main()
