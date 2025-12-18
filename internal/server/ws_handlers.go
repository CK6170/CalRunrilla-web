package server

import (
	"net/http"

	"github.com/gorilla/websocket"
)

// upgrader upgrades HTTP requests to WebSockets.
//
// Security note: CheckOrigin returns true to keep local development frictionless.
// This is acceptable for a local single-user app, but should be restricted if
// the server is ever exposed beyond localhost.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// local app; allow all
		return true
	},
}

// handleWSTest streams events produced by the "test weights" live mode.
func (s *Server) handleWSTest(w http.ResponseWriter, r *http.Request) {
	s.handleWSHub(w, r, s.wsTest)
}

// handleWSCal streams events produced during calibration sampling/compute/flash.
func (s *Server) handleWSCal(w http.ResponseWriter, r *http.Request) {
	s.handleWSHub(w, r, s.wsCal)
}

// handleWSFlash streams progress events produced by the explicit flash flow.
func (s *Server) handleWSFlash(w http.ResponseWriter, r *http.Request) {
	s.handleWSHub(w, r, s.wsFlash)
}

// handleWSHub is the shared "upgrade + register + read-loop" for all hubs.
//
// This endpoint does not currently handle incoming messages; the read-loop
// exists to detect client disconnects and trigger cleanup.
func (s *Server) handleWSHub(w http.ResponseWriter, r *http.Request, hub *WSHub) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := hub.Add(conn)

	// Keep reading until client disconnects
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			hub.Remove(client)
			return
		}
	}
}
