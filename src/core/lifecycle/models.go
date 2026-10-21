// Package lifecycle owns process-local, authenticated client leases.
package lifecycle

import "time"

const (
	Standalone            = "standalone"
	ClientManaged         = "client-managed"
	DefaultLeaseTTL       = 30 * time.Second
	DefaultStartupTimeout = 30 * time.Second
	MaxLeases             = 256
	MaxRecords            = 4096
)

// Options is a trusted host boundary, never decoded from an HTTP request.
type Options struct {
	Mode, InstanceID         string
	LeaseTTL, StartupTimeout time.Duration
	Now                      func() time.Time
}
type Principal struct {
	UserID, SessionID string
	ExpiresAt         time.Time
}
type Status struct {
	InstanceID      string     `json:"instanceId"`
	StartupMode     string     `json:"startupMode"`
	State           string     `json:"state"`
	ActiveLeases    int        `json:"activeLeases"`
	LeaseTTLMillis  int64      `json:"leaseTtlMs"`
	StartupDeadline *time.Time `json:"startupDeadline"`
	StartedAt       time.Time  `json:"startedAt"`
	DrainReason     *string    `json:"drainReason"`
}
type Lease struct {
	ID         string    `json:"leaseId"`
	InstanceID string    `json:"instanceId"`
	ClientID   string    `json:"clientId"`
	State      string    `json:"state"`
	Sequence   int64     `json:"sequence"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
type AcquireRequest struct {
	InstanceID string `json:"instanceId"`
	ClientID   string `json:"clientId"`
}
type RenewRequest struct {
	InstanceID string `json:"instanceId"`
	Sequence   int64  `json:"sequence"`
}
type ReleaseRequest struct {
	InstanceID string `json:"instanceId"`
}
