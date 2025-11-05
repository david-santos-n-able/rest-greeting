# REST Greeting Service

This project provides a minimal RESTful HTTP API with Prometheus instrumentation. It mirrors the gRPC example but exposes a `GET /hello` endpoint that responds with `"Hello <name>"` and emits traffic, error-rate, and latency metrics.

## Features

- `GET /hello?name=<value>` returns JSON greeting (defaults to `Hello World`)
- Prometheus counters and histograms instrumented via middleware
- Separate `/metrics` endpoint for scraping
- Graceful shutdown on `SIGINT`/`SIGTERM`

## Prerequisites

- Go 1.24+
- `curl` for sample requests

> **Note:** The first `go build`/`go run` will download Go module dependencies and populate `go.sum`.

## Running the Server

```sh
go run ./cmd/server
```

Default listen addresses:

- Application HTTP: `localhost:8080`
- Prometheus metrics: `localhost:9092`

Override as needed:

```sh
go run ./cmd/server --http-addr=:8081 --metrics-addr=:9100
```

## Example Requests

List the greeting using curl (plaintext JSON response):

```sh
curl 'http://localhost:8080/hello?name=Skaffold'
```

Expected JSON:

```json
{"message":"Hello Skaffold"}
```

Omit the name to use the default:

```sh
curl 'http://localhost:8080/hello'
```

## Scraping Metrics

Metrics are exported at `http://localhost:9092/metrics` in Prometheus format.

```sh
curl -s localhost:9092/metrics | grep http_requests_total
```

Latency histogram samples:

- `http_request_duration_seconds_bucket`
- `http_request_duration_seconds_sum`
- `http_request_duration_seconds_count`

These, alongside `http_requests_total`, give you traffic volume, status codes, and latency distribution.

## Project Layout

```
.
├── cmd/server          # REST server entrypoint
├── go.mod
├── go.sum
└── README.md
```

