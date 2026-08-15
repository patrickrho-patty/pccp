package scheduler

import (
	"time"

	"github.com/patrickrho-patty/pccp/internal/scheduler/queue"
)

// queueRequest adapts an S2 gateway request into a queue.Request. The
// queue package owns DRR; this mapping is the only bridge so class/tenant
// fields stay scheduler-owned.
func queueRequest(id, tenant, class, model string) queue.Request {
	return queue.Request{
		ID:                   id,
		Tenant:               tenant,
		Class:                queue.Class(class),
		InputTokens:          10,
		ExpectedOutputTokens: 10,
		ArrivedAt:            time.Now(),
		TTL:                  time.Minute,
		Payload:              RequestPayload{Model: model, Messages: []byte(`[]`)},
	}
}
