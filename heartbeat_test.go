package heartbeat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/twistingmercury/heartbeat"
)

func testServer(status int, delay bool) *httptest.Server {
	return httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if delay {
				time.Sleep(4 * time.Second)
			}
			w.WriteHeader(status)
			_, err := fmt.Fprintln(w, "Hello, client")
			if err != nil {
				return
			}
		}))
}

func TestInitialize(t *testing.T) {
	assert.Error(t, heartbeat.Initialize(nil, 1024, "unit", "heartbeat"))
	assert.Error(t, heartbeat.Initialize(context.TODO(), 1023, "unit", "heartbeat"))
	assert.Error(t, heartbeat.Initialize(context.TODO(), 49152, "unit", "heartbeat"))
	assert.Error(t, heartbeat.Initialize(context.TODO(), 49152, "", "heartbeat"))
	assert.Error(t, heartbeat.Initialize(context.TODO(), 8181, "unit", ""))
	assert.NoError(t, heartbeat.Initialize(context.TODO(), 8181, "unit", "heartbeat"))
}

func TestPublish(t *testing.T) {
	//ctx, _ := context.WithTimeout(context.Background(), 3*time.Second)
	ctx := context.Background()
	err := heartbeat.Initialize(ctx, 8181, "unit", "heartbeat")
	require.NoError(t, err)
	heartbeat.Publish()
	//<-ctx.Done()
	time.Sleep(2 * time.Second)
	err = heartbeat.Shutdown()
	require.NoError(t, err)
}

func TestDependencyHandlerFunc(t *testing.T) {
	exp := heartbeat.StatusResult{
		Status:          heartbeat.StatusOK,
		Name:            "Test Func",
		RequestDuration: 42,
		Resource:        "None",
	}
	dep := heartbeat.DependencyDescriptor{
		HandlerFunc: func() (hsr heartbeat.StatusResult) {
			return exp
		},
	}
	act := dep.HandlerFunc()

	assert.Equal(t, exp, act)
}

func TestCheckDepsInvokesHandlerFunc(t *testing.T) {
	exp := heartbeat.StatusResult{
		Status:          heartbeat.StatusOK,
		Name:            "Test Func",
		RequestDuration: 42,
		Resource:        "None",
	}

	ts := testServer(200, false)
	defer ts.Close()
	deps := []heartbeat.DependencyDescriptor{
		{Connection: ts.URL, Name: "Test URL 1", Type: "HTTP"},
		{Connection: "", Name: "Test custom", Type: "Custom", HandlerFunc: func() (hsr heartbeat.StatusResult) { return exp }},
	}

	s, r := heartbeat.CheckDeps(deps)
	assert.Equal(t, heartbeat.StatusOK, s)
	assert.Equal(t, 2, len(r))
}

func TestCheckUrlReturnsOK(t *testing.T) {
	ts := testServer(200, false)
	defer ts.Close()

	act := heartbeat.CheckURL(ts.URL)
	assert.Equal(t, heartbeat.StatusOK, act.Status)
}

func TestCheckURLReturnsError(t *testing.T) {
	act := heartbeat.CheckURL("hqpn://wtf.is.this.url???")
	assert.Equal(t, heartbeat.StatusCritical, act.Status)
}

func TestCheckUrlReturnWarning(t *testing.T) {
	ts := testServer(200, true)
	defer ts.Close()

	act := heartbeat.CheckURL(ts.URL)
	assert.Equal(t, heartbeat.StatusWarning, act.Status)
}

func TestCheckUrlReturnCritical(t *testing.T) {
	ts := testServer(500, false)
	defer ts.Close()

	act := heartbeat.CheckURL(ts.URL)
	assert.Equal(t, heartbeat.StatusCritical, act.Status)
}

func TestHandlerReturnCritical(t *testing.T) {
	tOK := testServer(200, false)
	tW1 := testServer(300, false)
	tW2 := testServer(200, true)
	tCrit := testServer(500, false)

	defer func() {
		tOK.Close()
		tW1.Close()
		tW2.Close()
		tCrit.Close()
	}()

	deps := []heartbeat.DependencyDescriptor{
		{Connection: tOK.URL, Name: "Test Good 1", Type: "HTTP"},
		{Connection: tCrit.URL, Name: "Test Critical 2", Type: "HTTP"},
		{Connection: tOK.URL, Name: "Test Good 3", Type: "HTTP"},
		{Connection: tW1.URL, Name: "Test Warn 300: SLOW", Type: "HTTP"},
		{Connection: tW2.URL, Name: "Test Warn 300", Type: "HTTP"},
	}

	resp := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	c, r := gin.CreateTestContext(resp)
	r.GET("/test", heartbeat.Handler("unit-test", deps...))
	c.Request, _ = http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(resp, c.Request)

	data := resp.Body.Bytes()
	assert.Greater(t, len(data), 0)

	var hcr heartbeat.Response

	err := json.Unmarshal(data, &hcr)
	assert.NoError(t, err)
	assert.Equal(t, heartbeat.StatusCritical, hcr.Status)

	str := string(data)
	exp := hcr.String()

	assert.Equal(t, exp, str)
}

func TestStatusString(t *testing.T) {
	assert.Equal(t, "OK", heartbeat.StatusOK.String())
	assert.Equal(t, "Warning", heartbeat.StatusWarning.String())
	assert.Equal(t, "Critical", heartbeat.StatusCritical.String())
	assert.Equal(t, "Status(5)", heartbeat.Status(5).String())
}

func TestStatusParse(t *testing.T) {
	ok, err := heartbeat.ParseStatus("OK")
	assert.NoError(t, err)
	assert.Equal(t, heartbeat.StatusOK, ok)

	wn, err := heartbeat.ParseStatus("Warning")
	assert.NoError(t, err)
	assert.Equal(t, heartbeat.StatusWarning, wn)

	cr, err := heartbeat.ParseStatus("Critical")
	assert.NoError(t, err)
	assert.Equal(t, heartbeat.StatusCritical, cr)

	x, err := heartbeat.ParseStatus("Fatal")
	assert.Error(t, err)
	assert.Equal(t, heartbeat.StatusNotSet, x)
}

func TestStatusUnmarshalText(t *testing.T) {
	var err error
	var hs heartbeat.Status
	err = hs.UnmarshalText([]byte("OK"))
	assert.NoError(t, err)

	err = hs.UnmarshalText([]byte("Warning"))
	assert.NoError(t, err)

	err = hs.UnmarshalText([]byte("Critical"))
	assert.NoError(t, err)

	err = hs.UnmarshalText([]byte("Fatal"))
	assert.Error(t, err)
}

func TestDependencyDescriptorString(t *testing.T) {
	desc := heartbeat.DependencyDescriptor{
		Connection:  "test",
		HandlerFunc: nil,
		Name:        "test",
		Type:        "test",
	}

	js := desc.String()

	assert.Greater(t, len(js), 0)
}

func TestStatusResultString(t *testing.T) {
	r := heartbeat.StatusResult{
		Status:          heartbeat.StatusOK,
		Name:            "test",
		RequestDuration: 42,
		Resource:        "test",
	}

	js := r.String()

	assert.Greater(t, len(js), 0)
}

func TestDependencyTypeString(t *testing.T) {
	assert.Equal(t, "OK", heartbeat.StatusOK.String())
	assert.Equal(t, "Warning", heartbeat.StatusWarning.String())
	assert.Equal(t, "Critical", heartbeat.StatusCritical.String())
}
