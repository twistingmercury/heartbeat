# Heartbeat

Heartbeat is a Go package that provides a simple way to add health check functionality to your web applications. It 
allows you to define dependencies that your service relies on and provides an endpoint to check the health status of 
your application and its dependencies.

## Installation

To install the Heartbeat package, run the following command:

```
go get github.com/twistingmercury/heartbeat
```

## Usage

1. Import the Heartbeat package in your Go application:

```go
import "github.com/twistingmercury/heartbeat"
```
## Defining Dependencies

Heartbeat allows you to define dependencies that your service relies on. These dependencies can be HTTP endpoints or
custom handler functions that check the status of other remote resources, such as a database, message queue, etc.

### HTTP Dependencies

Define your HTTP dependencies using the `DependencyDescriptor` struct by supplying a URL and a name for each dependency.

```go
dep01 := heartbeat.DependencyDescriptor{
    {
        Connection: "https://example.com",
        Name:       "Example Site",
        Type:       "Website",
    }
```
### Custom Depndencies

Define custom dependencies using the `DependencyDescriptor` struct by supplying a name and a handler function that is of
type `heartbeat.StatusHandlerFunc`:

```go
dep02 := heartbeat.DependencyDescriptor{
    {
        Name:        "My custom dependency",
        Type:        "My dependency",
        HandlerFunc: checkDependiency,
    }
	
func checkDependiency() heartbeat.StatusResult {
    // Check the status of your database connection
    // Return a StatusResult with the appropriate status and message
}
```
---

| Important! Please read                                                                                                                                                                                                                                                                    |
|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **While it is possible to define all your custom dependencies in a single function. I do not recommend this. It could cause you to possible lose the ability to determine which dependency is causing the issue. I encourage you to create a separate func for each dependency checked.** |

---

3. Register the health check endpoint in your application by providing your service's name, and by using the 
   `heartbeat.Handler` function. The `Handler` function, which takes the name of your service and a list of dependencies 
   as arguments. You can pass as many dependencies as you need. You also provide the name of your endpoint, in this example
   `"/healthcheck"` will be the endpoint to call.

```go
r.GET("/healthcheck", heartbeat.Handler("your-service-name", dep01, dep02)
```

5. Run your application and access the health check endpoint at `/healthcheck`.

## Health Status

The Heartbeat package defines the following health statuses:

- `NotSet`: The status has not been set.
- `OK`: The dependency is healthy.
- `Warning`: The dependency is experiencing issues but is still functioning.
- `Critical`: The dependency is not functioning properly.

## Response Format

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
      "resource": "",
      "request_duration_ms": 20,
      "http_status_code": 0,
      "message": "My custom dependency is healthy"
    }
  ]
}
```

The response includes the overall status of your application, along with the status of each defined dependency.

## Contributing

To work on the Heartbeat package, you'll need Go installed on your machine, as well as Docker and Docker Compose if you
want to run the example.

1. Clone the repository:

```bash
git clone https://github.com/twistingmercury/heartbeat.git
```

Contributions to the Heartbeat package are welcome! If you find any issues or have suggestions for improvements, 
please open an issue or submit a pull request on the [GitHub repository](https://github.com/twistingmercury/heartbeat).

## License

The Heartbeat package is open-source software released under the [MIT License](LICENSE).