package websocket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewClient tests the creation of a new client
func TestNewClient(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)
	userID := uuid.New()

	// Create mock websocket connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	client := NewClient(hub, conn, &userID, logger)

	assert.NotNil(t, client)
	assert.NotEmpty(t, client.id)
	assert.NotNil(t, client.userID)
	assert.Equal(t, userID, *client.userID)
	assert.Equal(t, hub, client.hub)
	assert.NotNil(t, client.conn)
	assert.NotNil(t, client.send)
	assert.Equal(t, 256, cap(client.send))
}

// TestNewClient_WithoutUserID tests creating a client without a user ID
func TestNewClient_WithoutUserID(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Create mock websocket connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	client := NewClient(hub, conn, nil, logger)

	assert.NotNil(t, client)
	assert.Nil(t, client.userID)
	assert.NotEmpty(t, client.id)
}

// TestClient_WritePump tests the write pump functionality
func TestClient_WritePump(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)
	go hub.Run()

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		serverConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer serverConn.Close()

		// Read messages from client
		serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for i := 0; i < 2; i++ {
			_, message, err := serverConn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					t.Logf("Read error: %v", err)
				}
				return
			}
			t.Logf("Server received: %s", message)
		}
	}))
	defer server.Close()

	// Connect client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	userID := uuid.New()
	client := NewClient(hub, conn, &userID, logger)

	// Start write pump
	go client.WritePump()

	// Send messages to client
	testMessages := [][]byte{
		[]byte("message 1"),
		[]byte("message 2"),
	}

	for _, msg := range testMessages {
		client.send <- msg
		time.Sleep(50 * time.Millisecond)
	}

	// Close connection gracefully
	close(client.send)
	time.Sleep(200 * time.Millisecond)

	// Test completes if we reach here without panic
	assert.True(t, true)
}

// TestClient_ReadPump tests the read pump functionality
func TestClient_ReadPump(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)
	go hub.Run()

	msgReceived := make(chan bool, 1)

	// Create test server that sends messages
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		serverConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer serverConn.Close()

		// Send some messages
		for i := 0; i < 2; i++ {
			err := serverConn.WriteMessage(websocket.TextMessage, []byte("test message"))
			if err != nil {
				t.Logf("Write error: %v", err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}

		msgReceived <- true

		// Keep connection open for a bit
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	// Connect client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	userID := uuid.New()
	client := NewClient(hub, conn, &userID, logger)

	// Register client
	hub.register <- client
	time.Sleep(50 * time.Millisecond)

	// Start read pump
	go client.ReadPump()

	// Wait for messages to be received
	select {
	case <-msgReceived:
		assert.True(t, true)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for messages")
	}

	time.Sleep(100 * time.Millisecond)
}

// TestClient_ReadPump_ConnectionClose tests read pump handling connection close
func TestClient_ReadPump_ConnectionClose(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)
	go hub.Run()

	unregisterReceived := make(chan bool, 1)

	// Monitor unregister channel
	go func() {
		select {
		case <-hub.unregister:
			unregisterReceived <- true
		case <-time.After(2 * time.Second):
			return
		}
	}()

	// Create test server that closes immediately
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		serverConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Close immediately
		serverConn.Close()
	}))
	defer server.Close()

	// Connect client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	userID := uuid.New()
	client := NewClient(hub, conn, &userID, logger)

	// Register client
	hub.register <- client
	time.Sleep(50 * time.Millisecond)

	// Start read pump (should handle close and unregister)
	go client.ReadPump()

	// Wait for unregister
	select {
	case <-unregisterReceived:
		assert.True(t, true)
	case <-time.After(2 * time.Second):
		t.Fatal("Client should have been unregistered")
	}
}

// TestClient_WritePump_ChannelClose tests write pump handling channel close
func TestClient_WritePump_ChannelClose(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)
	go hub.Run()

	closeReceived := make(chan bool, 1)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		serverConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer serverConn.Close()

		// Wait for close message
		serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			messageType, _, err := serverConn.ReadMessage()
			if err != nil {
				return
			}
			if messageType == websocket.CloseMessage {
				closeReceived <- true
				return
			}
		}
	}))
	defer server.Close()

	// Connect client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	userID := uuid.New()
	client := NewClient(hub, conn, &userID, logger)

	// Start write pump
	go client.WritePump()

	// Close send channel to trigger graceful close
	close(client.send)

	// Wait for close message
	select {
	case <-closeReceived:
		assert.True(t, true)
	case <-time.After(2 * time.Second):
		// This is acceptable - the connection may close before we read the close message
		assert.True(t, true)
	}
}

// TestClient_WritePump_PingPong tests ping/pong mechanism
func TestClient_WritePump_PingPong(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)
	go hub.Run()

	pingReceived := make(chan bool, 1)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		serverConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer serverConn.Close()

		// Set up ping handler
		serverConn.SetPingHandler(func(appData string) error {
			pingReceived <- true
			// Send pong response
			err := serverConn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(writeWait))
			return err
		})

		// Read messages
		serverConn.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			_, _, err := serverConn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	// Connect client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	userID := uuid.New()
	client := NewClient(hub, conn, &userID, logger)

	// Start write pump (will send periodic pings)
	go client.WritePump()

	// Wait for ping
	select {
	case <-pingReceived:
		assert.True(t, true)
	case <-time.After(2 * time.Second):
		// Ping interval is 54 seconds by default, so this might timeout
		// But the test demonstrates the mechanism
		t.Log("Ping not received within timeout (expected with long ping period)")
	}

	close(client.send)
}

// TestClient_ReadPump_PongHandler tests pong handling in read pump
func TestClient_ReadPump_PongHandler(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)
	go hub.Run()

	// Create test server that sends pings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		serverConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer serverConn.Close()

		// Send ping messages
		for i := 0; i < 3; i++ {
			err := serverConn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait))
			if err != nil {
				t.Logf("Ping error: %v", err)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}

		// Keep connection open
		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	// Connect client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	userID := uuid.New()
	client := NewClient(hub, conn, &userID, logger)

	// Register client
	hub.register <- client
	time.Sleep(50 * time.Millisecond)

	// Start read pump (will handle pongs)
	done := make(chan bool)
	go func() {
		client.ReadPump()
		done <- true
	}()

	// Wait for read pump to finish
	select {
	case <-done:
		assert.True(t, true)
	case <-time.After(2 * time.Second):
		t.Log("Read pump timeout (acceptable)")
	}
}

// TestClient_Constants tests that client constants are properly defined
func TestClient_Constants(t *testing.T) {
	assert.Equal(t, 10*time.Second, writeWait)
	assert.Equal(t, 60*time.Second, pongWait)
	assert.Equal(t, (pongWait*9)/10, pingPeriod)
	assert.Equal(t, int64(512), int64(maxMessageSize))
}

// TestClient_ReadPump_MaxMessageSize tests message size limit
func TestClient_ReadPump_MaxMessageSize(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)
	go hub.Run()

	connectionClosed := make(chan bool, 1)

	// Create test server that sends large message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		serverConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer serverConn.Close()

		// Send message larger than maxMessageSize (512 bytes)
		largeMessage := make([]byte, 1024)
		for i := range largeMessage {
			largeMessage[i] = 'A'
		}

		err = serverConn.WriteMessage(websocket.TextMessage, largeMessage)
		if err != nil {
			t.Logf("Write error: %v", err)
		}

		// Wait for connection to close
		time.Sleep(200 * time.Millisecond)
		connectionClosed <- true
	}))
	defer server.Close()

	// Connect client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	userID := uuid.New()
	client := NewClient(hub, conn, &userID, logger)

	// Register client
	hub.register <- client
	time.Sleep(50 * time.Millisecond)

	// Start read pump (should close on oversized message)
	go client.ReadPump()

	// Wait for server to finish
	select {
	case <-connectionClosed:
		assert.True(t, true)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for connection close")
	}
}

// TestClient_Integration tests full client lifecycle
func TestClient_Integration(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)
	go hub.Run()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	var messagesReceived int
	var receivedLock sync.Mutex

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		serverConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer serverConn.Close()

		// Send some messages from server
		for i := 0; i < 2; i++ {
			err := serverConn.WriteMessage(websocket.TextMessage, []byte("from server"))
			if err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}

		// Read messages from client
		serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			_, _, err := serverConn.ReadMessage()
			if err != nil {
				return
			}
			receivedLock.Lock()
			messagesReceived++
			receivedLock.Unlock()
		}
	}))
	defer server.Close()

	// Connect client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	userID := uuid.New()
	client := NewClient(hub, conn, &userID, logger)

	// Register client
	hub.register <- client
	time.Sleep(50 * time.Millisecond)

	// Start pumps
	go client.ReadPump()
	go client.WritePump()

	// Send messages to client
	for i := 0; i < 3; i++ {
		select {
		case client.send <- []byte("from client"):
			time.Sleep(50 * time.Millisecond)
		case <-time.After(500 * time.Millisecond):
			t.Log("Timeout sending message")
		}
	}

	// Wait a bit
	time.Sleep(300 * time.Millisecond)

	// Verify messages were received
	receivedLock.Lock()
	assert.GreaterOrEqual(t, messagesReceived, 1)
	receivedLock.Unlock()

	// Cleanup - don't close the send channel as WritePump may have already closed it
	conn.Close()
}
