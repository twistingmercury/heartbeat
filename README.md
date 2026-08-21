# Heartbeat

> **Maturity Level**: Basic - Production-ready and actively evolving.
>
> **Version**: v1.1.0

Heartbeat is a Go package for exposing Gin-based health endpoints. It checks
HTTP services and application-defined dependencies, then returns one aggregate
health response suitable for Kubernetes readiness and liveness probes.

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
- [Versioning](#versioning)
- [Contributing](#contributing)
- [License](#license)

## Usage

Install the package:

```bash
go get github.com/twistingmercury/heartbeat@v1.1.0
```

Define HTTP or custom dependencies and register the handler with Gin:

```go
package main

import (
    "time"

    "github.com/gin-gonic/gin"
    "github.com/twistingmercury/heartbeat"
)

func checkDatabase() heartbeat.StatusResult {
    // Replace this with a real database check.
    return heartbeat.StatusResult{
        Status:   heartbeat.StatusOK,
        Resource: "primary-database",
        Message:  "database is ready",
    }
}

func main() {
    router := gin.Default()

    dependencies := []heartbeat.DependencyDescriptor{
        {
            Name:       "Go website",
            Type:       "HTTP",
            Connection: "https://go.dev/",
            Timeout:    5 * time.Second,
        },
        {
            Name:        "Primary database",
            Type:        "Database",
            HandlerFunc: checkDatabase,
            Timeout:     2 * time.Second,
        },
    }

    router.GET("/health", heartbeat.Handler("example-service", dependencies...))
    _ = router.Run(":8080")
}
```

Request the endpoint with `curl http://localhost:8080/health`. A successful
response has this shape:

```json
{
  "status": "OK",
  "name": "example-service",
  "resource": "example-service",
  "machine": "hostname",
  "utc_DateTime": "2026-08-21T12:34:56Z",
  "request_duration_ms": 42.5,
  "dependencies": [
    {
      "status": "OK",
      "name": "Go website",
      "resource": "https://go.dev/",
      "request_duration_ms": 42.1,
      "http_status_code": 200,
      "message": "ok"
    },
    {
      "status": "OK",
      "name": "Primary database",
      "resource": "primary-database",
      "request_duration_ms": 0,
      "http_status_code": 0,
      "message": "database is ready"
    }
  ]
}
```

See the [example application](example/readme.md) for Cassandra and RabbitMQ
dependency checks.

## How it works

`heartbeat.Handler` runs all dependency checks concurrently while preserving
their declaration order in the response. HTTP checks inherit the request
context. Custom handlers run with timeout protection and panic recovery. The
most severe dependency status becomes the aggregate status:

| Status | Meaning | Endpoint HTTP status |
| --- | --- | --- |
| `NotSet` | No dependency established a status | 200 |
| `OK` | Healthy | 200 |
| `Warning` | Degraded but operational | 200 |
| `Critical` | Unhealthy | 503 |

HTTP dependency responses are classified as follows: 2xx is `OK`, 3xx is
`Warning`, and 4xx or 5xx is `Critical`. A successful response slower than
three seconds is also `Warning`. The standard Go client follows redirects, so
classification normally uses the final response.

## Key Considerations

- The default dependency timeout is 10 seconds. Set `Timeout` on each
  descriptor when a shorter limit is appropriate.
- HTTP dependencies accept only `http` and `https` URLs.
- A custom handler has no context parameter. Heartbeat can return when its
  timeout expires, but it cannot stop the handler goroutine; custom checks
  should therefore enforce cancellation in their own I/O operations.
- Panics in custom handlers are converted to `Critical` results instead of
  crashing the health endpoint.
- Use one custom handler per dependency so failures remain attributable.
- `Warning` deliberately returns HTTP 200. Use `Critical` when an orchestrator
  should remove the instance from service.

## Development Considerations

### Quick Start

The root module requires Go 1.26.6 or newer. Docker is required for
`make build-docker`; Docker with the Compose plugin is required for E2E commands.

```bash
git clone https://github.com/twistingmercury/heartbeat.git
cd heartbeat
go mod download
go test ./...
```

Useful project commands:

| Command | Purpose |
| --- | --- |
| `make test` | Run root unit tests and open an HTML coverage report |
| `make build` | Run unit tests, build the root package, and run E2E tests |
| `make build-docker` | Run unit tests and builds in the project build image |
| `make e2e-run` | Start dependencies, run E2E tests, and clean up |
| `make e2e-up` / `make e2e-down` | Manage the E2E environment manually |
| `make e2e-test` | Run E2E tests against manually started infrastructure |
| `make e2e-logs` / `make e2e-clean` | Inspect E2E logs or forcibly remove E2E resources |

Run the race detector directly when changing concurrent dependency handling:

```bash
go test -race ./...
```

### Testing

Unit tests use local HTTP test servers and require no external services. E2E
tests start Cassandra, RabbitMQ, and a consumer API with Docker Compose; initial
image pulls and Cassandra startup can take several minutes.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).
Version is determined from Git tags:

```bash
git describe --tags --always
```

Release history is maintained in [CHANGELOG.md](CHANGELOG.md).

## Contributing

Issues and pull requests are welcome in the
[GitHub repository](https://github.com/twistingmercury/heartbeat).

## License

Heartbeat is available under the [MIT License](LICENSE).
