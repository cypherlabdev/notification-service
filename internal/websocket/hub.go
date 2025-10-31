package websocket

import (
	"encoding/json"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Hub maintains active WebSocket connections
type Hub struct {
	clients    map[*Client]bool
	userConns  map[uuid.UUID][]*Client // userID -> connections
	broadcast  chan *Message
	register   chan *Client
	unregister chan *Client
	logger     zerolog.Logger
	mu         sync.RWMutex
}

// Message represents a WebSocket message
type Message struct {
	Type    string      `json:"type"`
	UserID  *uuid.UUID  `json:"user_id,omitempty"`
	Payload interface{} `json:"payload"`
}

// NewHub creates a new WebSocket hub
func NewHub(logger zerolog.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		userConns:  make(map[uuid.UUID][]*Client),
		broadcast:  make(chan *Message, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		logger:     logger.With().Str("component", "websocket_hub").Logger(),
	}
}

// Run starts the hub
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			if client.userID != nil {
				h.userConns[*client.userID] = append(h.userConns[*client.userID], client)
			}
			h.mu.Unlock()
			h.logger.Info().Str("client_id", client.id).Msg("client registered")

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)

				if client.userID != nil {
					conns := h.userConns[*client.userID]
					for i, c := range conns {
						if c == client {
							h.userConns[*client.userID] = append(conns[:i], conns[i+1:]...)
							break
						}
					}
				}
			}
			h.mu.Unlock()
			h.logger.Info().Str("client_id", client.id).Msg("client unregistered")

		case message := <-h.broadcast:
			h.broadcastMessage(message)
		}
	}
}

// BroadcastToUser sends a message to all connections of a specific user
func (h *Hub) BroadcastToUser(userID uuid.UUID, msgType string, payload interface{}) {
	msg := &Message{
		Type:    msgType,
		UserID:  &userID,
		Payload: payload,
	}
	h.broadcast <- msg
}

// BroadcastToAll sends a message to all connected clients
func (h *Hub) BroadcastToAll(msgType string, payload interface{}) {
	msg := &Message{
		Type:    msgType,
		Payload: payload,
	}
	h.broadcast <- msg
}

func (h *Hub) broadcastMessage(message *Message) {
	data, err := json.Marshal(message)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to marshal message")
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	if message.UserID != nil {
		// Send to specific user's connections
		for _, client := range h.userConns[*message.UserID] {
			select {
			case client.send <- data:
			default:
				// Client buffer full, skip
				h.logger.Warn().Str("client_id", client.id).Msg("client buffer full")
			}
		}
	} else {
		// Broadcast to all
		for client := range h.clients {
			select {
			case client.send <- data:
			default:
				h.logger.Warn().Str("client_id", client.id).Msg("client buffer full")
			}
		}
	}
}
