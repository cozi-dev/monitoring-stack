# Monitoring Demo

This project is a minimal end‑to‑end **observability stack** showcasing:

- **Metrics** with Prometheus
- **Logs** with Loki (via Promtail)
- **Traces** with Tempo
- **Dashboards & exploration** with Grafana

It includes small example applications (Go and Rust) that emit logs, metrics, and traces to demonstrate how to monitor and debug a distributed system.

## Stack Overview

- **Prometheus**: Scrapes metrics from the example services and system components.
- **Loki**: Stores logs collected by **Promtail** from Docker containers.
- **Tempo**: Receives distributed traces (spans) from the example services.
- **Grafana**: Single pane of glass for:
  - visualizing **metrics** (Prometheus)
  - exploring **logs** (Loki)
  - analyzing **traces** (Tempo)
  - using prebuilt dashboards (configured under `config/grafana`)

## Getting Started

Requirements:

- Docker
- Docker Compose

Start the whole monitoring stack:

```bash
docker-compose up -d
```

Once the containers are up:

- **Grafana UI**: http://localhost:13000
  - Default login is usually `admin` / `admin` (or as configured in `config/grafana/default.yaml`).
  - Preconfigured data sources: Prometheus, Loki, Tempo.

You can then:

- View metrics dashboards (Prometheus data source).
- Explore logs from services (Loki data source).
- Inspect distributed traces (Tempo data source).
- Correlate logs ↔ traces ↔ metrics from Grafana’s Explore view.

## Logging and Tracing in the Example Apps

- **Logging**:
  - Example services (Go and Rust) write logs to stdout.
  - **Promtail** reads Docker container logs and ships them to **Loki**.
  - In Grafana, select the **Loki** data source to query and filter logs by labels (service, container, etc.).

- **Tracing**:
  - Example services are instrumented to emit spans to **Tempo** (via OpenTelemetry or compatible clients).
  - In Grafana, select the **Tempo** data source to:
    - search for traces (by service, operation, duration, etc.)
    - drill down into spans to see timing and error details.
  - From a trace, you can pivot to related logs and metrics for full request‑level insight.

## References

- Logging with Docker, Promtail and Grafana Loki: https://ruanbekker.medium.com/logging-with-docker-promtail-and-grafana-loki-d920fd790ca8