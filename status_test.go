package heartbeat_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/twistingmercury/heartbeat/v2"
)

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
