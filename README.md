# Heartbeat

> **Maturity Level**: Basic - Production-ready and actively evolving.
>
> **Version**: v2.0.0

Heartbeat is a Go package for exposing health endpoints. `Handler` returns a
standard `http.Handler`, so it works with `net/http`, Gin, chi, Echo or any
other router, and adds no web framework to your dependencies. It checks
HTTP services and application-defined dependencies, then returns one aggregate
health response suitable for Kubernetes readiness and liveness probes.

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
  - [Quick Start](#quick-start)
  - [Testing](#testing)
  - [Versioning](#versioning)
- [Contributing](#contributing)
- [License](#license)

## Usage

Install the package:

```bash
go get github.com/twistingmercury/heartbeat/v2
```

Define HTTP or custom dependencies and register the handler with `net/http`:

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"

    "github.com/twistingmercury/heartbeat/v2"
)

func checkDatabase(ctx context.Context) heartbeat.StatusResult {
    // Replace this with a real database check that honors ctx.
    return heartbeat.StatusResult{
        Status:   heartbeat.StatusOK,
        Resource: "primary-database",
        Message:  "database is ready",
    }
}

func main() {
    dependencies := []heartbeat.DependencyDescriptor{
        {
            Name:       "Go website",
            Type:       "HTTP",
            Connection: "https://go.dev/",
            Timeout:    5 * time.Second,
        },
        {
            Name:      "Primary database",
            Type:      "Database",
            CheckFunc: checkDatabase,
            Timeout:   2 * time.Second,
        },
    }

    mux := http.NewServeMux()
    mux.Handle("GET /health", heartbeat.Handler("example-service", dependencies...))
    log.Fatal(http.ListenAndServe(":8080", mux))
}
```

`Handler` does not check the request method, so register it for `GET` on your
router as above.

### Using Heartbeat with Gin

Wrap the handler with `gin.WrapH`. Everything else stays the same:

```go
import (
    "github.com/gin-gonic/gin"
    "github.com/twistingmercury/heartbeat/v2"
)

router := gin.Default()
router.GET("/health", gin.WrapH(heartbeat.Handler("example-service", dependencies...)))
_ = router.Run(":8080")
```

Gin's middleware (logging, recovery, auth) runs before the handler as usual.
The [example application](example/README.md) and the E2E test API both serve
Heartbeat through Gin this way.

### Using Heartbeat with other routers

Any router that accepts an `http.Handler` can mount it directly, for example
`r.Method(http.MethodGet, "/health", h)` in chi or
`e.GET("/health", echo.WrapHandler(h))` in Echo.

### Upgrading from v1

v2 removes the Gin dependency. To upgrade:

1. Change the import path to `github.com/twistingmercury/heartbeat/v2`.
2. Wrap the handler: `router.GET("/health", heartbeat.Handler(...))` becomes
   `router.GET("/health", gin.WrapH(heartbeat.Handler(...)))`.
3. Read the timestamp from `utc_date_time` instead of `utc_DateTime`. In Go,
   the field is now `Response.UTCDateTime`.

Status codes, the other response keys and descriptor fields are unchanged.

Request the endpoint with `curl http://localhost:8080/health`. A successful
response has this shape:

```json
{
  "status": "OK",
  "name": "example-service",
  "resource": "example-service",
  "machine": "hostname",
  "utc_date_time": "2026-08-21T12:34:56Z",
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
      "request_duration_ms": 0.4,
      "http_status_code": 0,
      "message": "database is ready"
    }
  ]
}
```

To build your own response instead of using `Handler`, call
`heartbeat.CheckDependencies` directly. It runs the same concurrent, fail-closed
checks and returns the aggregate status with the per-dependency results in
declaration order. Unlike `Handler`, it reports a misconfigured descriptor as
`Critical` instead of panicking, and it does not share runs between concurrent
callers, so avoid calling it once per incoming probe:

```go
status, results := heartbeat.CheckDependencies(ctx, dependencies)
```

See the [example application](example/README.md) for Cassandra and RabbitMQ
dependency checks.

## How it works

`heartbeat.Handler` runs all dependency checks concurrently while preserving
their declaration order in the response. Custom checks run with timeout
protection and panic recovery. Concurrent requests share one run of the checks,
so a burst of probes makes one call to each dependency rather than one per
request. The most severe dependency status becomes the aggregate status:

| Status | Meaning | Endpoint HTTP status |
| --- | --- | --- |
| `OK` | Healthy, or no dependencies are configured | 200 |
| `Warning` | Degraded but operational | 200 |
| `Critical` | Unhealthy | 503 |

The health check fails closed. A custom check that returns `NotSet` (the zero
value) or any value outside this table is reported as `Critical`.

HTTP dependency responses are classified as follows: 2xx is `OK`, 3xx is
`Warning`, and 4xx or 5xx is `Critical`. A successful response slower than
three seconds (or the descriptor's `SlowThreshold`) is also `Warning`. The
standard Go client follows redirects, so classification normally uses the final
response.

## Key Considerations

- The default dependency timeout is 10 seconds. Set `Timeout` on each
  descriptor when a shorter limit is appropriate.
- `Handler` panics at startup if a descriptor has nothing to check, an invalid
  or non-HTTP URL, or a negative duration. HTTP dependencies accept only
  absolute `http` and `https` URLs.
- Use `CheckFunc` for custom checks and pass its context to your I/O so the
  check stops when its timeout expires. The older `HandlerFunc` is deprecated:
  it has no context, so a hung handler keeps its goroutine after the timeout.
  If both are set, `CheckFunc` wins, and either one takes precedence over
  `Connection`.
- Responses never include URL credentials or panic values, and error text for
  a malformed dependency URL never quotes any part of it. A failed request can
  still report its cause, which for a DNS failure may name the host. Panics are
  logged with `log/slog` and reported as `Critical`.
- Use one custom check per dependency so failures remain attributable.
- `Warning` deliberately returns HTTP 200. Use `Critical` when an orchestrator
  should remove the instance from service.

## Development Considerations

### Quick Start

The root module supports Go 1.26 or newer and builds with the Go 1.27.2
toolchain, which is also what the build images use.
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
| `make analyze` | Run `go vet`, the `goimports` format check, `golangci-lint`, `govulncheck` and `gosec` on the host without modifying files |
| `make test` | Run `make analyze`, then the root unit tests with the race detector and a coverage summary |

`make analyze` and `make test` need those tools installed on the host;
`make build` needs only Docker.

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
