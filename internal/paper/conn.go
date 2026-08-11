package paper

import (
	"errors"
	"fmt"
	"sync"
)

// Conn represents the state of a PAPER connection.
type Conn struct {
	mu    sync.RWMutex
	state ConnectionState

	// Negotiated parameters
	coreVersion     uint8
	extensions      map[string]uint8
	cryptoProfile   string
	transportBinding string

	// Authentication
	clientNonce []byte
	serverNonce []byte
	peerID      string
	orgID       string

	// Session
	sessionID    string
	leaseID      string
	policyEpoch  string
}

// NewConn creates a new connection in the NEW state.
func NewConn() *Conn {
	return &Conn{
		state:      StateNew,
		extensions: make(map[string]uint8),
	}
}

// State returns the current connection state.
func (c *Conn) State() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// Transition moves to a new state if the transition is valid (PAPER §14).
func (c *Conn) Transition(newState ConnectionState) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	valid := map[ConnectionState][]ConnectionState{
		StateNew:               {StateTransportReady},
		StateTransportReady:    {StateNegotiated, StateClosed},
		StateNegotiated:        {StatePeerAuthenticated, StateClosed},
		StatePeerAuthenticated: {StateIdentityBound, StateClosed},
		StateIdentityBound:     {StateReady, StateClosed},
		StateReady:             {StateDraining, StateClosed},
		StateDraining:          {StateClosed},
	}

	allowed, ok := valid[c.state]
	if !ok {
		return fmt.Errorf("paper: no transitions from %s", c.state)
	}

	for _, s := range allowed {
		if s == newState {
			c.state = newState
			return nil
		}
	}
	return fmt.Errorf("paper: invalid transition %s → %s", c.state, newState)
}

// SetNegotiated stores the negotiated parameters after HELLO/HELLO_ACK.
func (c *Conn) SetNegotiated(coreVer uint8, exts map[string]uint8, crypto, transport string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.coreVersion = coreVer
	c.extensions = exts
	c.cryptoProfile = crypto
	c.transportBinding = transport
}

// SetNonces stores the client and server nonces.
func (c *Conn) SetNonces(client, server []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clientNonce = client
	c.serverNonce = server
}

// SetPeerIdentity stores the authenticated peer identity.
func (c *Conn) SetPeerIdentity(peerID, orgID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.peerID = peerID
	c.orgID = orgID
}

// SetSession binds a working session to the connection.
func (c *Conn) SetSession(sessionID, leaseID, policyEpoch string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = sessionID
	c.leaseID = leaseID
	c.policyEpoch = policyEpoch
}

// PeerID returns the authenticated peer identifier.
func (c *Conn) PeerID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.peerID
}

// OrgID returns the authenticated organization.
func (c *Conn) OrgID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.orgID
}

// SessionID returns the bound working session.
func (c *Conn) SessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}

// CanAcceptApplicationMessages reports whether the connection is in READY state.
func (c *Conn) CanAcceptApplicationMessages() bool {
	return c.State() == StateReady
}

// ErrNotReady is returned when application messages are sent before READY.
var ErrNotReady = errors.New("paper: connection not in READY state")
