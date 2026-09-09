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

The root module requires Go 1.27.1 or newer for host-based development.
The standard build is Docker-first: install Docker with the Compose plugin to
run the complete validation suite without a host Go toolchain.

```bash
git clone https://github.com/twistingmercury/heartbeat.git
cd heartbeat
make build
```

Useful project commands:

| Command | Purpose |
| --- | --- |
| `make build` | Run the Docker-first quality gates, race tests, build, health-gated E2E tests, and cleanup |
| `make test` | Run host-based static analysis and root unit tests with an HTML coverage report |

Run the race detector directly when changing concurrent dependency handling:

```bash
go test -race ./...
```

### Testing

`make build` builds the root package in the project build image, then uses
Docker Compose to start Cassandra, RabbitMQ, and the consumer API. It waits for
each service's health check before running the E2E test container, propagates
the test result, and removes the Compose resources afterward. GitHub Actions
uses this same Docker-first build path. Initial image pulls and Cassandra startup
can take several minutes.

Host-based unit tests use local HTTP test servers and require no external
services:

```bash
go test ./...
```

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
