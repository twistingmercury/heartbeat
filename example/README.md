# Heartbeat Example Application

This application demonstrates an HTTP dependency and custom Cassandra and
RabbitMQ checks. It exposes Heartbeat through Gin at `GET /health`.

## Prerequisites

- Go 1.26.6 or newer when running from this repository's workspace
- Docker with the Compose plugin

## Run the Example

From a clone of the Heartbeat repository:

```bash
cd heartbeat/example
docker compose up -d
go run .
```

The application listens on `http://localhost:8080`. Query its health endpoint:

```bash
curl http://localhost:8080/health
```

Stop the application with `Ctrl+C`, then remove its dependencies:

```bash
docker compose down -v
```

## Dependencies

The descriptors in [main.go](main.go) check:

- the Go website over HTTPS;
- Cassandra through a custom handler; and
- RabbitMQ's management API through a custom handler.

The Compose environment publishes Cassandra on port `9042`, RabbitMQ's AMQP
port on `5672`, and RabbitMQ's management API on `15672`. Its development
credentials are defined in [docker-compose.yaml](docker-compose.yaml).

Both custom checks connect to `localhost`, so run the example application on
the host while the dependencies run in Docker. Cassandra may need a minute or
more before its check becomes healthy.

## Customize the Example

Edit the dependency descriptors or handler functions in [main.go](main.go).
Each handler returns a `heartbeat.StatusResult`; keep checks bounded with client
or driver timeouts so they cannot continue indefinitely after Heartbeat's
wrapper timeout expires.

For an entirely containerized consumer test, use the repository's E2E harness
instead:

```bash
cd ..
make e2e-run
```

## License

This example is available under the repository's [MIT License](../LICENSE).
