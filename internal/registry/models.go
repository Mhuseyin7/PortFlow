package registry

import "time"

// Service is a registered local domain → local port mapping.
type Service struct {
	ID         int64     `json:"id"`
	Project    string    `json:"project"`
	Hostname   string    `json:"hostname"`
	TargetHost string    `json:"target_host"`
	TargetPort int       `json:"target_port"`
	Protocol   string    `json:"protocol"` // "http" (only value in v1; TCP is future)
	TLS        bool      `json:"tls"`
	Status     string    `json:"status"` // UP, DOWN, STARTING, CONFLICT, MISCONFIGURED, UNKNOWN
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Valid status values.
const (
	StatusUp            = "UP"
	StatusDown          = "DOWN"
	StatusStarting      = "STARTING"
	StatusConflict      = "CONFLICT"
	StatusMisconfigured = "MISCONFIGURED"
	StatusUnknown       = "UNKNOWN"
)
