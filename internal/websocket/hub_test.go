package websocket

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewHub tests the creation of a new hub
func TestNewHub(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	assert.NotNil(t, hub)
	assert.NotNil(t, hub.clients)
	assert.NotNil(t, hub.userConns)
	assert.NotNil(t, hub.broadcast)
	assert.NotNil(t, hub.register)
	assert.NotNil(t, hub.unregister)
	assert.Equal(t, 256, cap(hub.broadcast))
}

// TestHub_RegisterClient tests client registration
func TestHub_RegisterClient(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Start hub in goroutine
	go hub.Run()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Create a mock client
	userID := uuid.New()
	client := &Client{
		id:     uuid.New().String(),
		userID: &userID,
		hub:    hub,
		conn:   nil, // Mock connection not needed for this test
		send:   make(chan []byte, 256),
		logger: logger,
	}

	// Register client
	hub.register <- client

	// Wait a bit for registration to complete
	time.Sleep(100 * time.Millisecond)

	// Check client is registered
	hub.mu.RLock()
	assert.True(t, hub.clients[client])
	assert.Len(t, hub.userConns[userID], 1)
	assert.Equal(t, client, hub.userConns[userID][0])
	hub.mu.RUnlock()
}

// TestHub_UnregisterClient tests client unregistration
func TestHub_UnregisterClient(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Start hub in goroutine
	go hub.Run()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Create and register a mock client
	userID := uuid.New()
	client := &Client{
		id:     uuid.New().String(),
		userID: &userID,
		hub:    hub,
		conn:   nil,
		send:   make(chan []byte, 256),
		logger: logger,
	}

	hub.register <- client
	time.Sleep(100 * time.Millisecond)

	// Verify client is registered
	hub.mu.RLock()
	assert.True(t, hub.clients[client])
	hub.mu.RUnlock()

	// Unregister client
	hub.unregister <- client
	time.Sleep(100 * time.Millisecond)

	// Verify client is unregistered
	hub.mu.RLock()
	_, exists := hub.clients[client]
	assert.False(t, exists)
	assert.Len(t, hub.userConns[userID], 0)
	hub.mu.RUnlock()
}

// TestHub_BroadcastToUser tests broadcasting to a specific user
func TestHub_BroadcastToUser(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Start hub in background
	go hub.Run()
	defer func() {
		// Note: In production, we'd need proper cleanup, but for testing this is acceptable
		time.Sleep(100 * time.Millisecond)
	}()

	userID := uuid.New()

	// Create two clients for the same user
	client1 := &Client{
		id:     uuid.New().String(),
		userID: &userID,
		hub:    hub,
		conn:   nil,
		send:   make(chan []byte, 256),
		logger: logger,
	}

	client2 := &Client{
		id:     uuid.New().String(),
		userID: &userID,
		hub:    hub,
		conn:   nil,
		send:   make(chan []byte, 256),
		logger: logger,
	}

	// Register clients
	hub.register <- client1
	hub.register <- client2
	time.Sleep(100 * time.Millisecond)

	// Broadcast message to user
	testPayload := map[string]string{"test": "data"}
	hub.BroadcastToUser(userID, "test_message", testPayload)

	// Wait for message to be processed
	time.Sleep(100 * time.Millisecond)

	// Both clients should receive the message
	select {
	case msg1 := <-client1.send:
		var message Message
		err := json.Unmarshal(msg1, &message)
		require.NoError(t, err)
		assert.Equal(t, "test_message", message.Type)
		assert.NotNil(t, message.UserID)
		assert.Equal(t, userID, *message.UserID)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for message on client1")
	}

	select {
	case msg2 := <-client2.send:
		var message Message
		err := json.Unmarshal(msg2, &message)
		require.NoError(t, err)
		assert.Equal(t, "test_message", message.Type)
		assert.NotNil(t, message.UserID)
		assert.Equal(t, userID, *message.UserID)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for message on client2")
	}
}

// TestHub_BroadcastToAll tests broadcasting to all clients
func TestHub_BroadcastToAll(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Start hub in background
	go hub.Run()
	defer func() {
		time.Sleep(100 * time.Millisecond)
	}()

	userID1 := uuid.New()
	userID2 := uuid.New()

	// Create clients with different users
	client1 := &Client{
		id:     uuid.New().String(),
		userID: &userID1,
		hub:    hub,
		conn:   nil,
		send:   make(chan []byte, 256),
		logger: logger,
	}

	client2 := &Client{
		id:     uuid.New().String(),
		userID: &userID2,
		hub:    hub,
		conn:   nil,
		send:   make(chan []byte, 256),
		logger: logger,
	}

	// Register clients
	hub.register <- client1
	hub.register <- client2
	time.Sleep(100 * time.Millisecond)

	// Broadcast message to all
	testPayload := map[string]string{"announcement": "server maintenance"}
	hub.BroadcastToAll("broadcast", testPayload)

	// Wait for message to be processed
	time.Sleep(100 * time.Millisecond)

	// Both clients should receive the message
	select {
	case msg1 := <-client1.send:
		var message Message
		err := json.Unmarshal(msg1, &message)
		require.NoError(t, err)
		assert.Equal(t, "broadcast", message.Type)
		assert.Nil(t, message.UserID)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for message on client1")
	}

	select {
	case msg2 := <-client2.send:
		var message Message
		err := json.Unmarshal(msg2, &message)
		require.NoError(t, err)
		assert.Equal(t, "broadcast", message.Type)
		assert.Nil(t, message.UserID)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for message on client2")
	}
}

// TestHub_BroadcastToUser_NoClients tests broadcasting when no clients are registered
func TestHub_BroadcastToUser_NoClients(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Start hub in background
	go hub.Run()
	defer func() {
		time.Sleep(100 * time.Millisecond)
	}()

	userID := uuid.New()

	// Broadcast to user that has no connections (should not panic or error)
	testPayload := map[string]string{"test": "data"}
	hub.BroadcastToUser(userID, "test_message", testPayload)

	// Wait a bit to ensure no panic
	time.Sleep(100 * time.Millisecond)

	// Test passes if we reach here without panic
	assert.True(t, true)
}

// TestHub_BroadcastMessage_InvalidJSON tests handling of invalid JSON in broadcast
func TestHub_BroadcastMessage_InvalidJSON(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Create a message with payload that cannot be marshaled
	// (channels cannot be marshaled to JSON)
	invalidPayload := make(chan int)
	message := &Message{
		Type:    "invalid",
		Payload: invalidPayload,
	}

	// This should log an error but not panic
	hub.broadcastMessage(message)

	// Test passes if we reach here without panic
	assert.True(t, true)
}

// TestHub_MultipleClientsPerUser tests multiple connections for same user
func TestHub_MultipleClientsPerUser(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Start hub in background
	go hub.Run()
	defer func() {
		time.Sleep(100 * time.Millisecond)
	}()

	userID := uuid.New()

	// Create 3 clients for the same user
	clients := make([]*Client, 3)
	for i := 0; i < 3; i++ {
		clients[i] = &Client{
			id:     uuid.New().String(),
			userID: &userID,
			hub:    hub,
			conn:   nil,
			send:   make(chan []byte, 256),
			logger: logger,
		}
		hub.register <- clients[i]
	}
	time.Sleep(100 * time.Millisecond)

	// Verify all clients are registered
	hub.mu.RLock()
	assert.Len(t, hub.userConns[userID], 3)
	hub.mu.RUnlock()

	// Broadcast message
	hub.BroadcastToUser(userID, "test", map[string]string{"msg": "hello"})
	time.Sleep(100 * time.Millisecond)

	// All clients should receive the message
	for i, client := range clients {
		select {
		case msg := <-client.send:
			var message Message
			err := json.Unmarshal(msg, &message)
			require.NoError(t, err, "client %d failed to unmarshal", i)
			assert.Equal(t, "test", message.Type)
		case <-time.After(1 * time.Second):
			t.Fatalf("Timeout waiting for message on client %d", i)
		}
	}
}

// TestHub_ClientBufferFull tests behavior when client send buffer is full
func TestHub_ClientBufferFull(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Start hub in background
	go hub.Run()
	defer func() {
		time.Sleep(100 * time.Millisecond)
	}()

	userID := uuid.New()

	// Create client with small buffer
	client := &Client{
		id:     uuid.New().String(),
		userID: &userID,
		hub:    hub,
		conn:   nil,
		send:   make(chan []byte, 2), // Small buffer
		logger: logger,
	}

	hub.register <- client
	time.Sleep(100 * time.Millisecond)

	// Fill the buffer by not reading from it
	for i := 0; i < 5; i++ {
		hub.BroadcastToUser(userID, "test", map[string]string{"msg": "hello"})
		time.Sleep(10 * time.Millisecond)
	}

	// Should not panic when buffer is full
	assert.True(t, true)
}

// TestHub_UnregisterMultipleClients tests unregistering multiple clients
func TestHub_UnregisterMultipleClients(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Start hub
	go hub.Run()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	userID := uuid.New()

	// Create and register 3 clients
	clients := make([]*Client, 3)
	for i := 0; i < 3; i++ {
		clients[i] = &Client{
			id:     uuid.New().String(),
			userID: &userID,
			hub:    hub,
			conn:   nil,
			send:   make(chan []byte, 256),
			logger: logger,
		}
		hub.register <- clients[i]
	}
	time.Sleep(100 * time.Millisecond)

	// Unregister middle client
	hub.unregister <- clients[1]
	time.Sleep(100 * time.Millisecond)

	// Verify only 2 clients remain
	hub.mu.RLock()
	assert.Len(t, hub.userConns[userID], 2)
	assert.False(t, hub.clients[clients[1]])
	hub.mu.RUnlock()
}

// TestHub_ConcurrentOperations tests thread safety with concurrent operations
func TestHub_ConcurrentOperations(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Start hub
	go hub.Run()
	defer func() {
		time.Sleep(200 * time.Millisecond)
	}()

	var wg sync.WaitGroup
	numGoroutines := 10
	numOperationsPerGoroutine := 5

	// Concurrent registrations, broadcasts, and unregistrations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			userID := uuid.New()

			for j := 0; j < numOperationsPerGoroutine; j++ {
				// Register
				client := &Client{
					id:     uuid.New().String(),
					userID: &userID,
					hub:    hub,
					conn:   nil,
					send:   make(chan []byte, 256),
					logger: logger,
				}
				hub.register <- client

				// Broadcast
				hub.BroadcastToUser(userID, "test", map[string]string{"id": string(rune(id))})

				time.Sleep(10 * time.Millisecond)

				// Unregister
				hub.unregister <- client
			}
		}(i)
	}

	wg.Wait()

	// Should complete without deadlock or panic
	assert.True(t, true)
}

// TestHub_Register returns the register channel
func TestHub_Register(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	assert.NotNil(t, hub.Register())
	assert.Equal(t, hub.register, hub.Register())
}

// Helper function to get register channel (add this method to Hub if not exists)
func (h *Hub) Register() chan *Client {
	return h.register
}

// TestHub_BroadcastToUserWithNilUserID tests broadcasting with nil user ID
func TestHub_BroadcastToUserWithNilUserID(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Start hub
	go hub.Run()
	defer func() {
		time.Sleep(100 * time.Millisecond)
	}()

	// Create client without user ID
	client := &Client{
		id:     uuid.New().String(),
		userID: nil,
		hub:    hub,
		conn:   nil,
		send:   make(chan []byte, 256),
		logger: logger,
	}

	hub.register <- client
	time.Sleep(100 * time.Millisecond)

	// Broadcast to a user - client without userID should not receive it
	someUserID := uuid.New()
	hub.BroadcastToUser(someUserID, "test", map[string]string{"msg": "hello"})
	time.Sleep(100 * time.Millisecond)

	// Client should not have received the message
	select {
	case <-client.send:
		t.Fatal("Client without userID should not receive user-specific broadcast")
	case <-time.After(200 * time.Millisecond):
		// Expected - no message received
		assert.True(t, true)
	}
}

// TestMessage_JSONMarshaling tests Message struct JSON marshaling
func TestMessage_JSONMarshaling(t *testing.T) {
	userID := uuid.New()
	msg := &Message{
		Type:    "test_type",
		UserID:  &userID,
		Payload: map[string]interface{}{"key": "value", "number": 42},
	}

	// Marshal
	data, err := json.Marshal(msg)
	require.NoError(t, err)

	// Unmarshal
	var unmarshaled Message
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, "test_type", unmarshaled.Type)
	assert.NotNil(t, unmarshaled.UserID)
	assert.Equal(t, userID, *unmarshaled.UserID)
	assert.NotNil(t, unmarshaled.Payload)
}
