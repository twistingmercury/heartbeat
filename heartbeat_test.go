package heartbeat_test

import (
	"encoding/json"
	"fmt"
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
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)

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

func TestCheckURLValidation(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		expectedStatus heartbeat.Status
		messageContain string
	}{
		{
			name:           "invalid URL scheme - file",
			url:            "file:///etc/passwd",
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "unsupported URL scheme",
		},
		{
			name:           "invalid URL scheme - ftp",
			url:            "ftp://example.com",
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "unsupported URL scheme",
		},
		{
			name:           "malformed URL",
			url:            "not a url at all",
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "unsupported URL scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := heartbeat.CheckURL(tt.url)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Contains(t, result.Message, tt.messageContain)
		})
	}
}

func TestCheckURL400StatusCodes(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		expectedStatus heartbeat.Status
		messageContain string
	}{
		{
			name:           "404 Not Found",
			statusCode:     404,
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "client error",
		},
		{
			name:           "401 Unauthorized",
			statusCode:     401,
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "client error",
		},
		{
			name:           "403 Forbidden",
			statusCode:     403,
			expectedStatus: heartbeat.StatusCritical,
			messageContain: "client error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := testServer(tt.statusCode, false)
			defer ts.Close()

			result := heartbeat.CheckURL(ts.URL)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Contains(t, result.Message, tt.messageContain)
		})
	}
}

func TestCheckURL300StatusCodes(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		expectedStatus heartbeat.Status
		messageContain string
	}{
		{
			name:           "301 Moved Permanently",
			statusCode:     301,
			expectedStatus: heartbeat.StatusWarning,
			messageContain: "redirect",
		},
		{
			name:           "302 Found",
			statusCode:     302,
			expectedStatus: heartbeat.StatusWarning,
			messageContain: "redirect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := testServer(tt.statusCode, false)
			defer ts.Close()

			result := heartbeat.CheckURL(ts.URL)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Contains(t, result.Message, tt.messageContain)
		})
	}
}
