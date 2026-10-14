package heartbeat

import "fmt"

// Status is the health of a service or one of its dependencies. Values are
// ordered by severity, so the aggregate status is the maximum.
type Status int

const (
	// StatusNotSet is the zero value. Checks that return it are reported as Critical.
	StatusNotSet Status = iota
	// StatusOK means healthy.
	StatusOK
	// StatusWarning means degraded but operational.
	StatusWarning
	// StatusCritical means unhealthy.
	StatusCritical
)

var statusNames = [...]string{
	StatusNotSet:   "NotSet",
	StatusOK:       "OK",
	StatusWarning:  "Warning",
	StatusCritical: "Critical",
}

// String implements the Stringer interface.
func (x Status) String() string {
	if x >= 0 && int(x) < len(statusNames) {
		return statusNames[x]
	}
	return fmt.Sprintf("Status(%d)", int(x))
}

// ParseStatus attempts to convert a string to a Status.
func ParseStatus(name string) (Status, error) {
	for i, n := range statusNames {
		if n == name {
			return Status(i), nil
		}
	}
	return StatusNotSet, fmt.Errorf("%s is not a valid Status", name)
}

// MarshalText implements the text marshaller method.
func (x Status) MarshalText() ([]byte, error) {
	return []byte(x.String()), nil
}

// UnmarshalText implements the text unmarshaller method.
func (x *Status) UnmarshalText(text []byte) error {
	tmp, err := ParseStatus(string(text))
	if err != nil {
		return err
	}
	*x = tmp
	return nil
}
