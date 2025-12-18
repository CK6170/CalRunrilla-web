package server

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

// WSMessage is the minimal event envelope sent over WebSocket.
//
// The frontend switches on `type` and treats `data` as an arbitrary JSON object.
type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
}

// WSClient wraps a websocket connection with a per-connection write mutex.
// Gorilla WebSocket requires that writes are not concurrent on the same Conn.
type WSClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// Send writes a message as JSON to this client.
func (c *WSClient) Send(msg WSMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(msg)
}

// WSHub is a lightweight broadcast hub for a set of WebSocket clients.
//
// This server is local + single-user, so a simple in-memory hub is enough.
// Broadcast intentionally marshals once per message and fan-outs the raw bytes
// to each client for consistency and efficiency.
type WSHub struct {
	mu      sync.RWMutex
	clients map[*WSClient]struct{}
}

// NewWSHub constructs an empty hub.
func NewWSHub() *WSHub {
	return &WSHub{clients: make(map[*WSClient]struct{})}
}

// Add registers a connection with the hub and returns the WSClient wrapper.
func (h *WSHub) Add(conn *websocket.Conn) *WSClient {
	c := &WSClient{conn: conn}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

// Remove unregisters a client and closes its connection.
func (h *WSHub) Remove(c *WSClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	_ = c.conn.Close()
}

// Broadcast sends a message to all connected clients.
//
// Note: failures are ignored; the read-loop in `handleWSHub` will eventually
// notice disconnects and remove the client. This keeps the broadcast path fast.
func (h *WSHub) Broadcast(msg WSMessage) {
	// Marshal once for consistency across clients
	b, _ := json.Marshal(msg)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		c.mu.Lock()
		_ = c.conn.WriteMessage(websocket.TextMessage, b)
		c.mu.Unlock()
	}
}
