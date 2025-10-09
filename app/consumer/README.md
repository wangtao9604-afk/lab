# qywx Consumer Service

This service hosts the legacy `Scheduler` runtime. It subscribes to `qywx-kf-ipang` (produced by the Producer service) and runs the downstream processing pipeline exactly as before.

## Key Responsibilities
- Start the scheduler, consume `schedule.kafkaTopic` (`qywx-kf-ipang`) via the existing kmq runtime.
- Keep business logic unchanged (processors, cursor management, etc.).
- Serve Prometheus metrics on `/metrics` using the same HTTP server (`services.consumer.httpAddr`).

## Configuration
- `services.consumer.httpAddr` — HTTP/metrics listener (defaults to `:11113`).
- `schedule.kafkaTopic` / `schedule.kafkaDlqTopic` — upstream topic & DLQ (`qywx-kf-ipang` / `qywx-kf-ipang-dlq`).

## Running Locally
```bash
GOOS=$(go env GOOS) GOARCH=$(go env GOARCH) go build -o bin/consumer ./app/consumer
./bin/consumer
```
