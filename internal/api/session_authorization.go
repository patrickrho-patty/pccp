package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/realtime"
)

const (
	permissionLiveRead       = "live:read"
	permissionLiveTranscript = "live:transcript"
	permissionSessionsManage = "sessions:manage"
)

func hasConsolePermission(r *http.Request, permission string) bool {
	claims, ok := claimsFromCtx(r.Context())
	if !ok {
		return false
	}
	// Transcript access is deliberately grant-only. Broad administrator roles
	// remain compatible for operational metadata and lifecycle control.
	if permission != permissionLiveTranscript {
		switch claims.Role {
		case "admin", "owner", "super_admin":
			return true
		}
	}
	for _, grant := range claims.Permissions {
		if grant == permission || (strings.HasPrefix(permission, "live:") && grant == "live:*") || (strings.HasPrefix(permission, "sessions:") && grant == "sessions:*") {
			return true
		}
	}
	return false
}

func (s *Server) requireConsolePermission(w http.ResponseWriter, r *http.Request, permission string) bool {
	if hasConsolePermission(r, permission) {
		return true
	}
	writeError(w, http.StatusForbidden, permission+" permission is required")
	return false
}

func (s *Server) handleRealtimeTicket(w http.ResponseWriter, r *http.Request) {
	if !s.requireConsolePermission(w, r, permissionLiveRead) {
		return
	}
	claims, ok := claimsFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "authenticated actor is required")
		return
	}
	actorID := strings.TrimSpace(claims.Subject)
	if actorID == "" {
		actorID = strings.TrimSpace(claims.Email)
	}
	if actorID == "" {
		writeError(w, http.StatusForbidden, "authenticated actor is required")
		return
	}
	token, expiresAt, err := s.ext().Realtime.IssueSSETicket(s.jwtSecret, realtime.StreamGrant{
		OrganizationID: getOrgID(r), ActorID: actorID, ActorEmail: strings.TrimSpace(claims.Email), UserID: strings.TrimSpace(claims.Subject),
		LifecycleEpoch:    claims.UserLifecycleEpoch,
		TranscriptVisible: hasConsolePermission(r, permissionLiveTranscript),
	}, time.Minute)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "could not issue live-stream ticket")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"stream_url":         "/api/realtime/sse?ticket=" + url.QueryEscape(token),
		"expires_at":         expiresAt.Format(time.RFC3339),
		"transcript_visible": hasConsolePermission(r, permissionLiveTranscript),
	})
}
