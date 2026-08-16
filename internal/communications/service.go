package communications

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Service implements the Enterprise Communications Hub (PRD §21-23).
type Service struct {
	db *gorm.DB
}

// New creates a new communications service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// CreateConversation creates a new conversation.
func (s *Service) CreateConversation(orgID, convType, title string, participants []string) (*models.Conversation, error) {
	partJSON, _ := json.Marshal(participants)
	conv := &models.Conversation{
		AuditBase: models.AuditBase{
			OrganizationID: orgID,
			Classification: "internal",
		},
		Type:             convType,
		Title:            title,
		ParticipantsJSON: string(partJSON),
		Status:           "active",
	}
	if err := s.db.Create(conv).Error; err != nil {
		return nil, fmt.Errorf("comms: create conversation: %w", err)
	}
	return conv, nil
}

// SendMessage sends a message in a conversation.
func (s *Service) SendMessage(convID, senderID, senderType, contentType, content string, parentID string) (*models.Message, error) {
	// The conversation must exist in the caller's org: messages are
	// org-scoped transitively through it (react/read/edit/delete all
	// resolve the conversation's org before mutating).
	var conv models.Conversation
	if err := s.db.Where("id = ?", convID).First(&conv).Error; err != nil {
		return nil, fmt.Errorf("comms: send message: conversation %s not found: %w", convID, err)
	}
	msg := &models.Message{
		ConversationID:  convID,
		SenderID:        senderID,
		SenderType:      senderType,
		ContentType:     contentType,
		Content:         content,
		ParentMessageID: parentID,
		DeliveredAt:     time.Now().Format(time.RFC3339),
	}

	if err := s.db.Create(msg).Error; err != nil {
		return nil, fmt.Errorf("comms: send message: %w", err)
	}

	// Update conversation last message time
	s.db.Model(&models.Conversation{}).Where("id = ?", convID).
		Update("last_message_at", time.Now().Format(time.RFC3339))

	return msg, nil
}

// ListMessages returns messages in a conversation.
func (s *Service) ListMessages(convID string, limit int) ([]models.Message, error) {
	if limit == 0 {
		limit = 50
	}
	var messages []models.Message
	err := s.db.Where("conversation_id = ?", convID).
		Order("created_at ASC").Limit(limit).Find(&messages).Error
	return messages, err
}

// UpdatePresence updates a user's presence status.
func (s *Service) UpdatePresence(orgID, userID, status, activity, harnessID string) error {
	presence := &models.Presence{
		OrganizationID: orgID,
		UserID:         userID,
		Status:         status,
		Activity:       activity,
		HarnessID:      harnessID,
		LastActiveAt:   time.Now().Format(time.RFC3339),
	}

	// Upsert
	result := s.db.Where("user_id = ? AND organization_id = ?", userID, orgID).
		Assign(map[string]interface{}{
			"status":         status,
			"activity":       activity,
			"harness_id":     harnessID,
			"last_active_at": time.Now().Format(time.RFC3339),
		}).FirstOrCreate(presence)

	return result.Error
}

// GetPresence returns presence for all users in an organization.
func (s *Service) GetPresence(orgID string) ([]models.Presence, error) {
	var presences []models.Presence
	err := s.db.Where("organization_id = ? AND status != 'offline'", orgID).Find(&presences).Error
	return presences, err
}

// CreateFileTransfer initiates a file transfer (PRD §23).
func (s *Service) CreateFileTransfer(orgID, senderID, recipientID, fileName string, fileSize int64, fileType, classification string) (*models.FileTransfer, error) {
	transfer := &models.FileTransfer{
		AuditBase: models.AuditBase{
			OrganizationID: orgID,
			Classification: classification,
		},
		SenderID:    senderID,
		RecipientID: recipientID,
		FileName:    fileName,
		FileSize:    fileSize,
		FileType:    fileType,
		Status:      "pending",
		ScanStatus:  "pending",
	}
	if err := s.db.Create(transfer).Error; err != nil {
		return nil, fmt.Errorf("comms: create file transfer: %w", err)
	}
	return transfer, nil
}

// CompleteFileTransfer marks a file transfer as completed.
func (s *Service) CompleteFileTransfer(transferID string) error {
	return s.db.Model(&models.FileTransfer{}).Where("id = ?", transferID).
		Updates(map[string]interface{}{
			"status":       "completed",
			"completed_at": time.Now().Format(time.RFC3339),
		}).Error
}

// SendBroadcast sends a broadcast message (PRD §22).
func (s *Service) SendBroadcast(orgID, severity, title, titleKo, body, bodyKo, targetType, targetID string, requiresAck bool) (*models.Broadcast, error) {
	broadcast := &models.Broadcast{
		AuditBase: models.AuditBase{
			OrganizationID: orgID,
		},
		Severity:    severity,
		Title:       title,
		TitleKo:     titleKo,
		Body:        body,
		BodyKo:      bodyKo,
		TargetType:  targetType,
		TargetID:    targetID,
		RequiresAck: requiresAck,
		Dismissable: severity != "emergency",
		Status:      "active",
	}
	if err := s.db.Create(broadcast).Error; err != nil {
		return nil, fmt.Errorf("comms: send broadcast: %w", err)
	}
	return broadcast, nil
}

// AckBroadcast records a user's acknowledgment of a broadcast.
func (s *Service) AckBroadcast(broadcastID, userID string) error {
	// Row-locked transaction: two concurrent acks otherwise read the
	// same AcksJSON and one is silently lost (lost update).
	return s.db.Transaction(func(tx *gorm.DB) error {
		var broadcast models.Broadcast
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", broadcastID).First(&broadcast).Error; err != nil {
			return err
		}
		var acks []string
		if broadcast.AcksJSON != "" {
			json.Unmarshal([]byte(broadcast.AcksJSON), &acks)
		}
		for _, a := range acks { // idempotent: re-ack is a no-op
			if a == userID {
				return nil
			}
		}
		acks = append(acks, userID)
		acksJSON, _ := json.Marshal(acks)
		return tx.Model(&broadcast).Updates(map[string]interface{}{
			"ack_count": len(acks),
			"acks_json": string(acksJSON),
		}).Error
	})
}

// LinkMessageToAIContext links a chat message to an AI session/exchange (PRD §21.6).
// This requires a separate Context Exchange before the content can enter AI context.
func (s *Service) LinkMessageToAIContext(messageID, sessionID, exchangeID string) error {
	return s.db.Model(&models.Message{}).Where("id = ?", messageID).
		Updates(map[string]interface{}{
			"linked_session_id":         sessionID,
			"linked_exchange_id":        exchangeID,
			"requires_context_exchange": true,
		}).Error
}

// ListConversations returns conversations for a user.
func (s *Service) ListConversations(orgID, userID string) ([]models.Conversation, error) {
	var conversations []models.Conversation
	// Find conversations where the user is a participant (stored as JSON array)
	s.db.Where("organization_id = ?", orgID).
		Where("participants LIKE ?", "%\""+userID+"\"%").
		Order("last_message_at DESC").Find(&conversations)
	return conversations, nil
}

// CreateFileTransferID generates a transfer ID.
func CreateFileTransferID() string {
	return dari.GenerateID("xfer")
}
