package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gocql/gocql"
	"github.com/twistingmercury/heartbeat/v2"
)

const checkTimeout = 2 * time.Second

var (
	cassandraHost = getEnv("CASSANDRA_HOST", "127.0.0.1")
	rabbitmqHost  = getEnv("RABBITMQ_HOST", "localhost")
	rabbitmqUser  = getEnv("RABBITMQ_USER", "rabbit")
	rabbitmqPass  = getEnv("RABBITMQ_PASS", "password")

	// httpClient is shared by every RabbitMQ check so connections are reused.
	// Per-request deadlines come from the check's context.
	httpClient = &http.Client{Timeout: 5 * time.Second}

	db = &cassandra{host: cassandraHost}
)

// getEnv returns the value of an environment variable or a default value if not set.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	// Connect once at startup. If Cassandra isn't reachable yet, the health
	// check retries the connection lazily.
	if _, err := db.get(); err != nil {
		log.Printf("cassandra not ready at startup, will retry from health check: %v", err)
	}
	defer db.close()

	r := gin.Default()

	// Define the dependencies that the service relies on
	deps := []heartbeat.DependencyDescriptor{
		{
			Name:       "Golang Site",
			Type:       "Website",
			Connection: "https://go.dev/",
			Timeout:    checkTimeout,
			// Report Warning instead of OK when the site answers slower than this.
			SlowThreshold: time.Second,
		},
		{
			Name:      "database check",
			Type:      "database",
			CheckFunc: checkDB,
			Timeout:   checkTimeout,
		},
		{
			Name:      "RabbitMQ check",
			Type:      "RabbitMQ",
			CheckFunc: checkRMQ,
			Timeout:   checkTimeout,
		},
	}

	// Register the healthcheck endpoint by passing the name of the service
	r.GET("/health", gin.WrapH(heartbeat.Handler("example", deps...)))
	if err := r.Run(); err != nil {
		log.Fatal(err)
	}
}

// cassandra holds a single shared session, created on first successful connect.
type cassandra struct {
	host    string
	mu      sync.Mutex
	session *gocql.Session
}

func (c *cassandra) get() (*gocql.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil && !c.session.Closed() {
		return c.session, nil
	}
	cluster := gocql.NewCluster(c.host)
	cluster.Keyspace = "system"
	cluster.Consistency = gocql.One
	cluster.ConnectTimeout = checkTimeout
	cluster.Timeout = checkTimeout
	s, err := cluster.CreateSession()
	if err != nil {
		return nil, err
	}
	c.session = s
	return s, nil
}

func (c *cassandra) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		c.session.Close()
	}
}

func checkDB(ctx context.Context) heartbeat.StatusResult {
	session, err := db.get()
	if err != nil {
		return critical(err.Error())
	}
	if err := session.Query(`SELECT release_version FROM system.local`).WithContext(ctx).Exec(); err != nil {
		return critical(err.Error())
	}
	return heartbeat.StatusResult{Status: heartbeat.StatusOK, Message: "database is ready"}
}

func checkRMQ(ctx context.Context) heartbeat.StatusResult {
	url := fmt.Sprintf("http://%s:15672/api/aliveness-test/%%2F", rabbitmqHost)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return critical(err.Error())
	}
	req.SetBasicAuth(rabbitmqUser, rabbitmqPass)

	resp, err := httpClient.Do(req)
	if err != nil {
		return critical(err.Error())
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return critical(fmt.Sprintf("RabbitMQ is not healthy: HTTP %d", resp.StatusCode))
	}
	return heartbeat.StatusResult{Status: heartbeat.StatusOK, Message: "RabbitMQ is healthy"}
}

func critical(msg string) heartbeat.StatusResult {
	return heartbeat.StatusResult{Status: heartbeat.StatusCritical, Message: msg}
}
