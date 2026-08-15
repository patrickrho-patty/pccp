package scheduler

// RegisterPayload is the payload of MsgEndpointRegister: the freshly signed
// capability card plus the CP-signed worker config (present only on full
// registration; heartbeats reuse the connection's stored config).
type RegisterPayload struct {
	Card   WorkerCard    `json:"card"`
	Config *SignedConfig `json:"config,omitempty"`
}

// HeartbeatPayload is the payload of MsgEndpointLease: a freshly signed card
// renewing the worker's lease. The card is the health signal.
type HeartbeatPayload struct {
	Card WorkerCard `json:"card"`
}

// RegisterAckPayload is the scheduler's reply to both register and heartbeat.
// Denied requests carry no lease; quarantined requests carry a lease but are
// flagged non-compliant (excluded from serving pools once routing exists).
type RegisterAckPayload struct {
	Outcome         Outcome `json:"outcome"`
	WorkerID        string  `json:"worker_id"`
	LeaseTTLSeconds int     `json:"lease_ttl_seconds"`
	Reason          string  `json:"reason,omitempty"`
}
