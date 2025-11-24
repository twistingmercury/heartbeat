package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gocql/gocql"

	"log"

	"github.com/gin-gonic/gin"
	"github.com/twistingmercury/heartbeat"
)

// getEnv returns the value of an environment variable or a default value if not set.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	r := gin.Default()

	// Define the dependencies that the service relies on
	deps := []heartbeat.DependencyDescriptor{
		{
			Connection: "https://golang.org/",
			Name:       "Golang Site",
			Type:       "Website",
		},
		{
			Name:        "database check",
			Type:        "database",
			HandlerFunc: checkDB,
		},
		{
			Name:        "RabbitMQ check",
			Type:        "RabbitMQ",
			HandlerFunc: checkRMQ,
		},
	}

	// Register the healthcheck endpoint by passing the name of the service
	r.GET("/health", heartbeat.Handler("testApi", deps...))
	if err := r.Run(); err != nil {
		log.Fatal(err)
	}
}

//goland:noinspection ALL
func checkDB() heartbeat.StatusResult {
	hsr := heartbeat.StatusResult{
		Status:  heartbeat.StatusOK,
		Message: "database is ready",
	}

	cassandraHost := getEnv("CASSANDRA_HOST", "127.0.0.1")
	cluster := gocql.NewCluster(cassandraHost)
	cluster.Keyspace = "system"
	cluster.Consistency = gocql.Quorum
	session, err := cluster.CreateSession()

	if err != nil {
		hsr.Status = heartbeat.StatusCritical
		hsr.Message = err.Error()
		return hsr
	}
	defer session.Close()
	err = session.Query(`SELECT release_version FROM system.local;`).Exec()
	if err != nil {
		hsr.Status = heartbeat.StatusCritical
		hsr.Message = err.Error()
	}

	return hsr
}

func checkRMQ() heartbeat.StatusResult {
	client := http.Client{
		Timeout: 1 * time.Second,
	}
	defer client.CloseIdleConnections()
	rabbitmqHost := getEnv("RABBITMQ_HOST", "localhost")
	rabbitmqURL := fmt.Sprintf("http://rabbit:password@%s:15672/api/aliveness-test/%%2F", rabbitmqHost)
	req, err := http.NewRequest("GET", rabbitmqURL, nil)
	if err != nil {
		return heartbeat.StatusResult{
			Status:  heartbeat.StatusCritical,
			Message: err.Error(),
		}
	}

	resp, err := client.Do(req)

	switch {
	case err != nil:
		return heartbeat.StatusResult{
			Status:  heartbeat.StatusCritical,
			Message: err.Error(),
		}
	case resp.StatusCode != http.StatusOK:
		return heartbeat.StatusResult{
			Status:  heartbeat.StatusCritical,
			Message: "RabbitMQ is not healthy",
		}
	default:
		return heartbeat.StatusResult{
			Status:  heartbeat.StatusOK,
			Message: "RabbitMQ is healthy",
		}
	}
}
