package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// comms_extra.go: web/13 — SSE fan-out (A1), threading/mentions/
// reactions/read receipts (B1/B2), AI-context linking (B4), broadcast
// ack dashboard (B5), 1:1 from user search (C1), privacy gating (C2),
// system commands (C3), real file transfer storage + scan + download
// (A3/C4), retention purge (C5).

// commsBroadcast fans an event out to the org's SSE subscribers.
func (s *Server) commsBroadcast(orgID, eventType string, payload interface{}) {
	if s.ext().Realtime != nil {
		s.ext().Realtime.BroadcastToOrg(orgID, eventType, payload)
	}
}

// handleSendMessageExtended replaces handleSendMessage with the rich
// variant: mentions, content_type, context exchange, threading, and
// role-gated system commands (C3).
func (s *Server) handleSendMessageExtended(w http.ResponseWriter, r *http.Request) {
	convID := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	role := getRole(r)
	var req struct {
		SenderID                string   `json:"sender_id"`
		SenderType              string   `json:"sender_type"`
		Content                 string   `json:"content"`
		ContentType             string   `json:"content_type"`
		ParentID                string   `json:"parent_id,omitempty"`
		Mentions                []string `json:"mentions"`
		LinkedSessionID         string   `json:"linked_session_id,omitempty"`
		LinkedExchangeID        string   `json:"linked_exchange_id,omitempty"`
		RequiresContextExchange bool     `json:"requires_context_exchange,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SenderType == "" {
		req.SenderType = "user"
	}
	// C3: system/command messages require an elevated operator.
	if req.SenderType == "system" && role != "admin" && role != "owner" {
		writeError(w, http.StatusForbidden, "시스템 명령은 admin/owner만 발신할 수 있습니다 · system commands require admin/owner")
		return
	}
	if req.ContentType == "" {
		req.ContentType = "text"
	}
	if req.ContentType == "command" && req.SenderType != "system" {
		writeError(w, http.StatusBadRequest, "command content requires sender_type=system")
		return
	}
	msg, err := s.comms.SendMessage(convID, req.SenderID, req.SenderType, req.ContentType, req.Content, req.ParentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// B2/B4: mentions, context links, context-exchange requirement.
	if len(req.Mentions) > 0 {
		raw, _ := json.Marshal(req.Mentions)
		msg.MentionsJSON = string(raw)
	}
	if req.LinkedSessionID != "" || req.LinkedExchangeID != "" {
		_ = s.comms.LinkMessageToAIContext(msg.ID, req.LinkedSessionID, req.LinkedExchangeID)
	}
	if req.RequiresContextExchange {
		msg.RequiresContextExchange = true
	}
	if msg.MentionsJSON != "" || msg.RequiresContextExchange {
		s.db.Save(msg)
	}
	s.commsBroadcast(orgID, "comms.message", map[string]interface{}{
		"conversation_id": convID, "message": msg,
	})
	writeJSON(w, http.StatusCreated, msg)
}

// handleMessageReact toggles a reaction (B2).
func (s *Server) handleMessageReact(w http.ResponseWriter, r *http.Request) {
	msgID := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		Emoji  string `json:"emoji"`
		UserID string `json:"user_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Emoji == "" {
		writeError(w, http.StatusBadRequest, "emoji + user_id required")
		return
	}
	var msg models.Message
	if err := s.db.First(&msg, "id = ?", msgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	// Org scope resolves through the conversation (Message carries no
	// org column): a cross-org message id is indistinguishable from a
	// missing one.
	var msgConv models.Conversation
	if err := s.db.Where("id = ? AND organization_id = ?", msg.ConversationID, orgID).
		First(&msgConv).Error; err != nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	reactions := map[string][]string{}
	if msg.ReactionsJSON != "" {
		_ = json.Unmarshal([]byte(msg.ReactionsJSON), &reactions)
	}
	users := reactions[req.Emoji]
	found := false
	kept := users[:0]
	for _, u := range users {
		if u == req.UserID {
			found = true
			continue
		}
		kept = append(kept, u)
	}
	if found {
		reactions[req.Emoji] = kept
		if len(kept) == 0 {
			delete(reactions, req.Emoji)
		}
	} else {
		reactions[req.Emoji] = append(kept, req.UserID)
	}
	raw, _ := json.Marshal(reactions)
	s.db.Model(&msg).Update("reactions", string(raw))
	s.commsBroadcast(orgID, "comms.reaction", map[string]interface{}{
		"message_id": msgID, "reactions": reactions,
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"reactions": reactions})
}

// handleMessageRead marks a message read (B2).
func (s *Server) handleMessageRead(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	msgID := chi.URLParam(r, "id")
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	var msg models.Message
	if err := s.db.First(&msg, "id = ?", msgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	// Org scope resolves through the conversation (Message carries no
	// org column): a cross-org message id is indistinguishable from a
	// missing one.
	var msgConv models.Conversation
	if err := s.db.Where("id = ? AND organization_id = ?", msg.ConversationID, orgID).
		First(&msgConv).Error; err != nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	readers := []string{}
	if msg.ReadByJSON != "" {
		_ = json.Unmarshal([]byte(msg.ReadByJSON), &readers)
	}
	for _, u := range readers {
		if u == req.UserID {
			writeJSON(w, http.StatusOK, map[string]interface{}{"read_by": readers})
			return
		}
	}
	readers = append(readers, req.UserID)
	raw, _ := json.Marshal(readers)
	s.db.Model(&msg).Update("read_by", string(raw))
	writeJSON(w, http.StatusOK, map[string]interface{}{"read_by": readers})
}

// handleMessageEdit edits a message (Edited=true) (UX14).
func (s *Server) handleMessageEdit(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	msgID := chi.URLParam(r, "id")
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content required")
		return
	}
	var msg models.Message
	if err := s.db.First(&msg, "id = ?", msgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	// Org scope resolves through the conversation (Message carries no
	// org column): a cross-org message id is indistinguishable from a
	// missing one.
	var msgConv models.Conversation
	if err := s.db.Where("id = ? AND organization_id = ?", msg.ConversationID, orgID).
		First(&msgConv).Error; err != nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	msg.Content = req.Content
	msg.Edited = true
	s.db.Save(&msg)
	writeJSON(w, http.StatusOK, msg)
}

// handleMessageDelete soft-deletes a message (DeletedBy) (UX14).
func (s *Server) handleMessageDelete(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	msgID := chi.URLParam(r, "id")
	var req struct {
		DeletedBy string `json:"deleted_by"`
	}
	_ = decodeJSON(r, &req)
	var msg models.Message
	if err := s.db.First(&msg, "id = ?", msgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	// Org scope resolves through the conversation (Message carries no
	// org column): a cross-org message id is indistinguishable from a
	// missing one.
	var msgConv models.Conversation
	if err := s.db.Where("id = ? AND organization_id = ?", msg.ConversationID, orgID).
		First(&msgConv).Error; err != nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	s.db.Model(&msg).Update("deleted_by", req.DeletedBy)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleMessageLink binds a message to AI context (B4).
func (s *Server) handleMessageLink(w http.ResponseWriter, r *http.Request) {
	msgID := chi.URLParam(r, "id")
	var req struct {
		SessionID  string `json:"session_id"`
		ExchangeID string `json:"exchange_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.comms.LinkMessageToAIContext(msgID, req.SessionID, req.ExchangeID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "linked"})
}

// handleBroadcastAcks returns the ack dashboard for a broadcast (B5).
func (s *Server) handleBroadcastAcks(w http.ResponseWriter, r *http.Request) {
	broadcastID := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var bc models.Broadcast
	if err := s.db.First(&bc, "id = ? AND organization_id = ?", broadcastID, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "broadcast not found")
		return
	}
	var ackedIDs []string
	if bc.AcksJSON != "" {
		_ = json.Unmarshal([]byte(bc.AcksJSON), &ackedIDs)
	}
	ackedBy := map[string]bool{}
	for _, a := range ackedIDs {
		ackedBy[a] = true
	}
	var users []models.User
	s.db.Where("organization_id = ? AND status = 'active'", orgID).Find(&users)
	var pending []map[string]string
	ackedCount := 0
	for _, u := range users {
		if ackedBy[u.ID] {
			ackedCount++
		} else {
			pending = append(pending, map[string]string{"user_id": u.ID, "name": u.Name, "name_ko": u.NameKo, "email": u.Email})
		}
	}
	rate := 0.0
	if len(users) > 0 {
		rate = float64(ackedCount) / float64(len(users))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"broadcast_id": broadcastID,
		"requires_ack": bc.RequiresAck,
		"total_users":  len(users),
		"acked":        ackedCount,
		"pending":      pending,
		"ack_rate":     rate,
	})
}

// handleBroadcastAck records one user's ack (B5).
func (s *Server) handleBroadcastAck(w http.ResponseWriter, r *http.Request) {
	broadcastID := chi.URLParam(r, "id")
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if err := s.comms.AckBroadcast(broadcastID, req.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "acked"})
}

// handleConversationDM finds-or-creates a 1:1 DM between the operator
// and a developer (C1) — the launch point from user search.
func (s *Server) handleConversationDM(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	var target models.User
	if err := s.db.First(&target, "id = ? AND organization_id = ?", req.UserID, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	// Operator identity: admin credential email as the operator-side
	// participant (console operators are not in the users table).
	operatorID := "operator:" + getOperatorEmail(r)
	conversations, _ := s.comms.ListConversations(orgID, "")
	for _, c := range conversations {
		if c.Type == "direct" {
			var participants []string
			_ = json.Unmarshal([]byte(c.ParticipantsJSON), &participants)
			if containsStr(participants, operatorID) && containsStr(participants, req.UserID) {
				writeJSON(w, http.StatusOK, c)
				return
			}
		}
	}
	conv, err := s.comms.CreateConversation(orgID, "direct", "", []string{operatorID, req.UserID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, conv)
}

func getOperatorEmail(r *http.Request) string {
	if claims, ok := claimsFromCtx(r.Context()); ok {
		return claims.Email
	}
	return "unknown"
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// --- File transfer reality (A3/C4) ---

func commsStorageDir() string {
	if dir := os.Getenv("PCCP_COMMS_STORAGE"); dir != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), "pccp-comms")
}

// handleFileTransferUpload stores the file content, hashes it, scans it
// with the security content checks, and flips the transfer to ready or
// blocked (A3).
func (s *Server) handleFileTransferUpload(w http.ResponseWriter, r *http.Request) {
	transferID := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var transfer models.FileTransfer
	if err := s.db.First(&transfer, "id = ? AND organization_id = ?", transferID, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "transfer not found")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "multipart field 'file' required")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 64<<20))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir := commsStorageDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sum := sha256.Sum256(content)
	hashHex := hex.EncodeToString(sum[:])
	path := filepath.Join(dir, transferID+"-"+sanitizeName(header.Filename))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Scan: content inspection via the security service (secrets/PII/
	// sensitive paths) + size sanity. Findings block the transfer.
	check := s.security.CheckContext(orgID, string(content))
	findingsJSON, _ := json.Marshal(check.Findings)
	scanStatus := "clean"
	if len(check.Findings) > 0 {
		scanStatus = "blocked"
	}
	if err := s.db.Model(&transfer).Updates(map[string]interface{}{
		"storage_path":       path,
		"file_hash":          hashHex,
		"file_size":          int64(len(content)),
		"file_type":          header.Filename[strings.LastIndex(header.Filename, ".")+1:],
		"scan_status":        scanStatus,
		"scan_findings_json": string(findingsJSON),
		"status":             map[bool]string{true: "ready", false: "rejected"}[scanStatus == "clean"],
	}).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.commsBroadcast(orgID, "comms.transfer", map[string]interface{}{
		"transfer_id": transferID, "scan_status": scanStatus,
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"transfer_id": transferID, "scan_status": scanStatus, "findings": check.Findings,
	})
}

// handleFileTransferDownload streams a clean, unexpired transfer (A3).
func (s *Server) handleFileTransferDownload(w http.ResponseWriter, r *http.Request) {
	transferID := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var transfer models.FileTransfer
	if err := s.db.First(&transfer, "id = ? AND organization_id = ?", transferID, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "transfer not found")
		return
	}
	if transfer.ScanStatus != "clean" {
		writeError(w, http.StatusForbidden, "파일이 검사를 통과하지 않았습니다 · file failed content scan")
		return
	}
	// gorm's timestamp column stores the Go zero time as a non-empty
	// string — treat it as unset.
	if transfer.ExpiresAt != "" && transfer.ExpiresAt != "0001-01-01T00:00:00Z" &&
		transfer.ExpiresAt < time.Now().Format(time.RFC3339) {
		writeError(w, http.StatusGone, "전송이 만료되었습니다 · transfer expired")
		return
	}
	if transfer.StoragePath == "" {
		writeError(w, http.StatusNotFound, "content not uploaded")
		return
	}
	http.ServeFile(w, r, transfer.StoragePath)
}

// handleFileTransferTransition implements accept/decline/complete (C4).
func (s *Server) handleFileTransferTransition(w http.ResponseWriter, r *http.Request) {
	transferID := chi.URLParam(r, "id")
	orgID := getOrgID(r)
	var req struct {
		Action string `json:"action"` // accept, decline, complete
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var transfer models.FileTransfer
	if err := s.db.First(&transfer, "id = ? AND organization_id = ?", transferID, orgID).Error; err != nil {
		writeError(w, http.StatusNotFound, "transfer not found")
		return
	}
	switch req.Action {
	case "accept":
		if transfer.Status == "ready" {
			s.db.Model(&transfer).Update("status", "downloading")
		}
	case "decline":
		s.db.Model(&transfer).Update("status", "rejected")
	case "complete":
		s.db.Model(&transfer).Updates(map[string]interface{}{
			"status": "completed", "completed_at": time.Now().Format(time.RFC3339),
		})
	default:
		writeError(w, http.StatusBadRequest, "action must be accept|decline|complete")
		return
	}
	writeJSON(w, http.StatusOK, transfer)
}

// sweepCommsRetention purges soft-deleted messages + expired transfers
// (C5). Called from the API ticker.
func (s *Server) sweepCommsRetention() int {
	n := 0
	// Soft-deleted messages older than 30 days are purged.
	res := s.db.Where("deleted_by != '' AND updated_at < ?", time.Now().Add(-30*24*time.Hour)).
		Delete(&models.Message{})
	n += int(res.RowsAffected)
	// Expired transfers: delete content + row after 7 days past expiry.
	var expired []models.FileTransfer
	s.db.Where("expires_at != '' AND expires_at != '0001-01-01T00:00:00Z' AND expires_at < ?",
		time.Now().Format(time.RFC3339)).Find(&expired)
	for _, t := range expired {
		if t.StoragePath != "" {
			_ = os.Remove(t.StoragePath)
		}
		s.db.Delete(&t)
		n++
	}
	return n
}

func sanitizeName(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", "..", "_")
	return replacer.Replace(name)
}
