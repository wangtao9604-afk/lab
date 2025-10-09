# qywx Producer Service

This service exposes the Enterprise WeCom callback endpoints, writes raw callbacks to `wx_raw_event`, and (when elected leader via Redis fetcher) pulls real KF messages to fan-out into `qywx-kf-ipang`.

## Key Responsibilities
- Validate/decrypt WeCom callbacks via the existing `controllers` package, then push raw envelopes to `wx_raw_event`.
- Run `KFService` + fetcher leader election to consume `wx_raw_event`, fetch customer messages, and produce them to `schedule.kafkaTopic` (`qywx-kf-ipang`).
- Serve Prometheus metrics on `/metrics` (listening on `services.producer.httpAddr`).

## Configuration
- `services.producer.httpAddr` — HTTP/metrics listener (defaults to `:11112`).
- `kafka.brokers`, `kafka.producer.*` — Kafka client settings.
- `kafka.topics.callbackInbound` — raw callback topic (defaults to `wx_raw_event`).
- `schedule.kafkaTopic` / `schedule.kafkaDlqTopic` — fan-out target & DLQ (`qywx-kf-ipang` / `qywx-kf-ipang-dlq`).

## Running Locally
```bash
GOOS=$(go env GOOS) GOARCH=$(go env GOARCH) go build -o bin/producer ./app/producer
./bin/producer
```
