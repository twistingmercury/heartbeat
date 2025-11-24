# Heartbeat

> **Maturity Level**: Basic - Ready for production use.
> panic recovery, and stable API

A Go package providing health check functionality with support for HTTP and
custom dependency monitoring.

## Usage

Import the Heartbeat package in your Go application:

```go
import "github.com/twistingmercury/heartbeat"
```

### Defining Dependencies

Heartbeat allows you to define dependencies that your service relies on. These
dependencies can be HTTP endpoints or custom handler functions that check the
status of other remote resources, such as a database, message queue, etc.

#### HTTP Dependencies

Define your HTTP dependencies using the `DependencyDescriptor` struct by
supplying a URL and a name for each dependency. You can optionally specify a
timeout duration for the dependency check.

```go
dep01 := heartbeat.DependencyDescriptor{
    Connection: "https://example.com",
    Name:       "Example Site",
    Type:       "Website",
    Timeout:    5 * time.Second, // Optional: defaults to 10 seconds if not set
}
```

#### Custom Dependencies

Define custom dependencies using the `DependencyDescriptor` struct by supplying
a name and a handler function that is of type `heartbeat.StatusHandlerFunc`:

```go
dep02 := heartbeat.DependencyDescriptor{
    Name:        "My custom dependency",
    Type:        "My dependency",
    HandlerFunc: checkDependency,
}

func checkDependency() heartbeat.StatusResult {
    // Check the status of your database connection
    // Return a StatusResult with the appropriate status and message
}
```

> **Important**: While it is possible to define all your custom dependencies in
a single function, I do not recommend this. It could cause you to possibly lose
the ability to determine which dependency is causing the issue. I encourage you
to create a separate func for each dependency checked.

### Registering the Handler

Register the health check endpoint in your application by providing your
service's name, and by using the `heartbeat.Handler` function. The `Handler`
function takes the name of your service and a list of dependencies as
arguments. You can pass as many dependencies as you need. You also provide the
name of your endpoint, in this example `"/healthcheck"` will be the endpoint
to call.

```go
r.GET("/healthcheck", heartbeat.Handler("your-service-name", dep01, dep02))
```

Run your application and access the health check endpoint at `/healthcheck`.

### Response Format

The health check endpoint returns a JSON response with the following structure:

```json
{
  "status": "OK",
  "name": "your-service-name",
  "resource": "your-service-name",
  "machine": "hostname",
  "utc_DateTime": "2023-05-08T12:34:56Z",
  "request_duration_ms": 100,
  "message": "Service is healthy",
  "dependencies": [
    {
      "status": "OK",
      "name": "Example Site",
      "resource": "https://example.com",
      "request_duration_ms": 50,
      "http_status_code": 200,
      "message": "ok"
    },
    {
      "status": "OK",
      "name": "My custom dependency",
      "resource": "My custom dependency",
      "request_duration_ms": 20,
      "http_status_code": 0,
      "message": "My custom dependency is healthy"
    }
  ]
}
```

The response includes the overall status of your application, along with the
status of each defined dependency.

### Health Status Values

The Heartbeat package defines the following health statuses:

- `NotSet`: The status has not been set.
- `OK`: The dependency is healthy.
- `Warning`: The dependency is experiencing issues but is still functioning.
- `Critical`: The dependency is not functioning properly.

### HTTP Status Codes

The HTTP status code of the response is determined by the overall health status:

- `200 OK`: When the overall status is `NotSet`, `OK`, or `Warning`
- `503 Service Unavailable`: When the overall status is `Critical`

For HTTP dependencies, the `http_status_code` field contains the actual HTTP
status code returned by the dependency. For custom dependencies, this field is
set to `0`.

## How it works

Heartbeat provides a simple HTTP handler that aggregates health status from
multiple dependencies. When the health check endpoint is called, it evaluates
all registered dependencies in parallel, each with its own timeout protection.
HTTP dependencies are checked by making requests to their configured URLs,
while custom dependencies execute user-provided handler functions. The overall
service health is determined by the most severe status among all dependencies.

Custom handler functions are protected with automatic panic recovery to prevent
crashes from unexpected errors in user code. Context cancellation and timeout
handling ensure that checks respect configured time limits and respond properly
to cancelled requests.

The package uses Go's standard HTTP client for HTTP dependencies and supports
both synchronous and asynchronous health checks. Response times are measured
for each dependency to help identify performance issues.

## Key Considerations

- **Kubernetes Integration**: This package is designed to work as an HTTP
  [readiness and liveness probe](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#http-probes)
  for Kubernetes deployments
- **Timeout Protection**: All HTTP dependency checks have configurable timeouts
  (defaults to 10 seconds) to prevent hanging connections
- **TLS Support**: HTTPS endpoints are fully supported for secure communication
  with dependencies
- **Custom Dependency Isolation**: Create separate handler functions for each
  custom dependency to maintain clear error attribution and easier
  troubleshooting

## Development Considerations

### Quick Start

Requires Go 1.24+ - [Installation instructions](https://golang.org/doc/install)

Clone the repository:

```bash
git clone https://github.com/twistingmercury/heartbeat.git
cd heartbeat
```

### Building & running

Build the package:

```bash
go build
```

To run the example application:

```bash
cd example
go run main.go
```

The example requires Docker and Docker Compose if you want to test with
containerized dependencies.

### Testing

Run the test suite:

```bash
go test ./...
```

Run tests with coverage:

```bash
go test -cover ./...
```

### Versioning

This project uses git tag-based versioning following semantic versioning
principles. Release tags follow the format `vX.Y.Z` (e.g., `v1.2.3`).

## Contributing

Contributions to the Heartbeat package are welcome! If you find any issues or
have suggestions for improvements, please open an issue or submit a pull
request on the
[GitHub repository](https://github.com/twistingmercury/heartbeat).

## License

The Heartbeat package is open-source software released under the [MIT License](LICENSE).
