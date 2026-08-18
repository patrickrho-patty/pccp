package relay

import (
	"context"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm/clause"
)

const relayControlTTL = 5 * time.Minute

func (s *Service) QueueDirectiveEvent(orgID, harnessID, commandType, reason string, body []byte) (*models.RelayControlEvent, error) {
	event := &models.RelayControlEvent{
		OrganizationID: orgID, HarnessID: harnessID, Kind: "directive",
		CommandType: commandType, Reason: reason, Body: append([]byte(nil), body...),
		ExpiresAt: time.Now().UTC().Add(relayControlTTL),
	}
	if err := s.db.Create(event).Error; err != nil {
		return nil, fmt.Errorf("relay: queue directive: %w", err)
	}
	return event, nil
}

func (s *Service) QueueRevocationEvent(orgID, harnessID, reason string) (*models.RelayControlEvent, error) {
	event := &models.RelayControlEvent{
		OrganizationID: orgID, HarnessID: harnessID, Kind: "revocation", Reason: reason,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	if err := s.db.Create(event).Error; err != nil {
		return nil, fmt.Errorf("relay: queue revocation: %w", err)
	}
	return event, nil
}

func (s *Service) AckControlEvent(eventID string, delivered int) {
	ack := &models.RelayControlAck{EventID: eventID, RelayID: s.relayID, Delivered: delivered, AppliedAt: time.Now().UTC()}
	_ = s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(ack).Error
}

// StartControlEventWorker consumes the shared control carrier on every relay
// replica. A load balancer may route the enqueue request anywhere; the replica
// owning the target connection still observes and delivers the same row.
func (s *Service) StartControlEventWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.processControlEvents(time.Now().UTC())
			}
		}
	}()
}

func (s *Service) processControlEvents(now time.Time) {
	var events []models.RelayControlEvent
	acked := s.db.Model(&models.RelayControlAck{}).Select("event_id").Where("relay_id = ?", s.relayID)
	if err := s.db.Where("expires_at > ? AND id NOT IN (?)", now, acked).
		Order("created_at ASC").Limit(100).Find(&events).Error; err != nil {
		return
	}
	for _, event := range events {
		delivered := 0
		switch event.Kind {
		case "revocation":
			if err := s.RevokeHarness(event.OrganizationID, event.HarnessID, event.Reason); err != nil {
				continue
			}
			delivered = 1
		case "directive":
			delivered = s.DeliverDirectiveToHarness(event.HarnessID, event.Body)
			if delivered == 0 {
				continue
			}
		default:
			continue
		}
		s.AckControlEvent(event.ID, delivered)
	}
	// Bounded retention; acknowledgements cascade only logically, so remove
	// their expired parents' rows in two bounded statements.
	var expired []string
	if err := s.db.Model(&models.RelayControlEvent{}).Where("expires_at <= ?", now).Limit(500).Pluck("id", &expired).Error; err == nil && len(expired) > 0 {
		s.db.Unscoped().Where("event_id IN ?", expired).Delete(&models.RelayControlAck{})
		s.db.Unscoped().Where("id IN ?", expired).Delete(&models.RelayControlEvent{})
	}
}
