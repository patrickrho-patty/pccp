package realtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

// Service implements real-time WebSocket/SSE for live updates (PRD §21, §14).
// Provides live session monitoring, presence, chat, and fleet status.
type Service struct {
	mu      sync.RWMutex
	clients map[string]*Client // clientID → client
}

// Client represents a connected WebSocket client.
type Client struct {
	ID            string          `json:"i_d"`
	UserID        string          `json:"user_i_d"`
	OrgID         string          `json:"org_i_d"`
	Conn          *websocket.Conn `json:"conn"`
	Send          chan []byte     `json:"send"`
	Subscriptions map[string]bool `json:"subscriptions"` // event types subscribed to
}

// Event is a real-time event pushed to clients.
type Event struct {
	Type    string      `json:"type"` // session.update, presence.update, chat.message, fleet.action, security.finding
	Payload interface{} `json:"payload"`
	Time    string      `json:"time"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Production would verify origin
	},
}

// New creates a new real-time service.
func New() *Service {
	return &Service{
		clients: make(map[string]*Client),
	}
}

// HandleWebSocket upgrades HTTP to WebSocket and manages the connection.
func (s *Service) HandleWebSocket(jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Authenticate via query parameter token
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			tokenStr = r.Header.Get("Authorization")
			if len(tokenStr) > 7 {
				tokenStr = tokenStr[7:]
			}
		}

		claims := &jwt.RegisteredClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		client := &Client{
			ID:            fmt.Sprintf("ws_%d", time.Now().UnixNano()),
			Conn:          wsConn,
			Send:          make(chan []byte, 256),
			Subscriptions: make(map[string]bool),
		}

		s.mu.Lock()
		s.clients[client.ID] = client
		s.mu.Unlock()

		// Read pump
		go s.readPump(client)
		// Write pump
		go s.writePump(client)

		// Send welcome event
		client.Send <- mustMarshal(Event{
			Type:    "connection.established",
			Payload: map[string]string{"client_id": client.ID, "status": "연결됨 (connected)"},
			Time:    time.Now().Format(time.RFC3339),
		})
	}
}

// HandleSSE handles Server-Sent Events as an alternative to WebSocket.
func (s *Service) HandleSSE(jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.URL.Query().Get("token")
		claims := &jwt.RegisteredClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		clientID := fmt.Sprintf("sse_%d", time.Now().UnixNano())
		ch := make(chan []byte, 256)

		// Initial flush so the client knows the connection is established.
		fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		s.mu.Lock()
		s.clients[clientID] = &Client{
			ID:   clientID,
			Send: ch,
		}
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			delete(s.clients, clientID)
			s.mu.Unlock()
		}()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-time.After(30 * time.Second):
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

// Broadcast sends an event to all connected clients.
func (s *Service) Broadcast(eventType string, payload interface{}) {
	event := Event{
		Type:    eventType,
		Payload: payload,
		Time:    time.Now().Format(time.RFC3339),
	}
	data := mustMarshal(event)

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, client := range s.clients {
		select {
		case client.Send <- data:
		default:
			// Buffer full, skip
		}
	}
}

// BroadcastToOrg sends an event to clients in a specific organization.
func (s *Service) BroadcastToOrg(orgID string, eventType string, payload interface{}) {
	event := Event{
		Type:    eventType,
		Payload: payload,
		Time:    time.Now().Format(time.RFC3339),
	}
	data := mustMarshal(event)

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, client := range s.clients {
		if client.OrgID == orgID {
			select {
			case client.Send <- data:
			default:
			}
		}
	}
}

// NotifySessionUpdate pushes a session state update.
func (s *Service) NotifySessionUpdate(orgID, sessionID, status string) {
	s.BroadcastToOrg(orgID, "session.update", map[string]string{
		"session_id": sessionID,
		"status":     status,
	})
}

// NotifySecurityFinding pushes a security finding to admins.
func (s *Service) NotifySecurityFinding(orgID, severity, titleKo string) {
	s.BroadcastToOrg(orgID, "security.finding", map[string]string{
		"severity": severity,
		"title_ko": titleKo,
	})
}

// NotifyChatMessage pushes a new chat message.
func (s *Service) NotifyChatMessage(orgID, conversationID, senderName, content string) {
	s.BroadcastToOrg(orgID, "chat.message", map[string]string{
		"conversation_id": conversationID,
		"sender":          senderName,
		"content":         content,
	})
}

// NotifyFleetAction pushes a fleet action result.
func (s *Service) NotifyFleetAction(orgID, action, harnessID string) {
	s.BroadcastToOrg(orgID, "fleet.action", map[string]string{
		"action":     action,
		"harness_id": harnessID,
	})
}

// ConnectedClients returns the count of connected clients.
func (s *Service) ConnectedClients() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// SetupRoutes registers the WebSocket/SSE routes on a chi router.
func (s *Service) SetupRoutes(r chi.Router, jwtSecret string) {
	r.Get("/ws", s.HandleWebSocket(jwtSecret).ServeHTTP)
	r.Get("/sse", s.HandleSSE(jwtSecret).ServeHTTP)
}

func (s *Service) readPump(client *Client) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, client.ID)
		s.mu.Unlock()
		client.Conn.Close()
	}()

	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			break
		}

		// Parse subscription messages
		var msg struct {
			Action string   `json:"action"` // subscribe, unsubscribe
			Types  []string `json:"types"`
		}
		if json.Unmarshal(message, &msg) == nil {
			switch msg.Action {
			case "subscribe":
				for _, t := range msg.Types {
					client.Subscriptions[t] = true
				}
			case "unsubscribe":
				for _, t := range msg.Types {
					delete(client.Subscriptions, t)
				}
			}
		}
	}
}

func (s *Service) writePump(client *Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		client.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.Send:
			if !ok {
				client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			client.Conn.WriteMessage(websocket.TextMessage, message)
		case <-ticker.C:
			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func mustMarshal(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
