# Example Heartbeat Application

This is an example application that demonstrates how to use the Heartbeat package to add health check functionality to 
a Go web application. The application relies on a Cassandra database and a RabbitMQ message broker, which are defined 
as dependencies in the health check.

## Prerequisites

- Go 1.21 or higher
- Docker and Docker Compose

## Getting Started

1. Clone the repository:

```
git clone https://github.com/your-username/example-heartbeat-app.git
```

2. Change to the project directory:

```
cd example-heartbeat-app
```

3. Start the Cassandra and RabbitMQ containers using Docker Compose:

```
docker-compose up
```

4. Run the application:

```
go run main.go
```

The application will start running on `http://localhost:8080`.

## Health Check Endpoint

The application exposes a health check endpoint at `/healthcheck`. You can access it using a web browser or by sending an HTTP GET request:

```
curl http://localhost:8080/healthcheck
```

The endpoint will return a JSON response with the health status of the application and its dependencies.

## Dependencies

The example application defines the following dependencies:

- Golang Site: Checks the availability of the Go language website.
- Database Check: Verifies the connection to the Cassandra database.
- RabbitMQ Check: Checks the health of the RabbitMQ message broker.

These dependencies are defined in the `main.go` file using the `heartbeat.DependencyDescriptor` struct.

## Configuration

The application configuration is defined in the `docker-compose.yaml` file. It specifies the Cassandra and RabbitMQ containers along with their respective configurations.

- Cassandra:
    - Image: `cassandra:latest`
    - Container Name: `cassandra-container`
    - Ports: `9042:9042`
    - Environment Variables:
        - `CASSANDRA_USER=cassandra`
        - `CASSANDRA_PASSWORD=cassandra`

- RabbitMQ:
    - Image: `rabbitmq:3-management-alpine`
    - Container Name: `rabbitmq`
    - Ports: `5672:5672`, `15672:15672`
    - Environment Variables:
        - `RABBITMQ_DEFAULT_USER=rabbit`
        - `RABBITMQ_DEFAULT_PASS=password`

## Customization

You can customize the example application by modifying the following:

- Dependencies: Add, remove, or modify the dependencies defined in the `main.go` file.
- Handler Functions: Implement custom handler functions for your dependencies in the `main.go` file.
- Configuration: Update the configuration in the `docker-compose.yaml` file to match your environment.

## Contributing

If you find any issues or have suggestions for improvements, please open an issue or submit a pull request on the [GitHub repository](https://github.com/your-username/example-heartbeat-app).

## License

This example application is open-source software released under the [MIT License](../LICENSE).