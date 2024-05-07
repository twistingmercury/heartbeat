//go:generate go-enum -f=$GOFILE --marshal

package heartbeat

// HealthStatus defines the health statuses.
/* ENUM(
NotSet
OK
Warning
Critical
)
*/
type Status int
