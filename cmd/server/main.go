package main

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	ws "github.com/cypherlabdev/notification-service/internal/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // TODO: Implement proper origin checking
	},
}

func main() {
	logger := zerolog.New(os.Stdout).
		With().
		Timestamp().
		Str("service", "notification-service").
		Logger()

	log.Logger = logger
	logger.Info().Msg("notification-service starting")

	// Create WebSocket hub
	hub := ws.NewHub(logger)
	go hub.Run()

	// HTTP handlers
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(hub, w, r, logger)
	})
	http.Handle("/metrics", promhttp.Handler())

	// Start server
	go func() {
		logger.Info().Int("port", 8084).Msg("HTTP server listening")
		if err := http.ListenAndServe(":8084", nil); err != nil {
			logger.Fatal().Err(err).Msg("server failed")
		}
	}()

	// TODO: Start Kafka consumer to receive events and broadcast to clients
	// Consumer would listen to topics: wallet-events, order-events, match-events
	// and call hub.BroadcastToUser() for relevant notifications

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	logger.Info().Msg("shutting down")
}

func serveWS(hub *ws.Hub, w http.ResponseWriter, r *http.Request, logger zerolog.Logger) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error().Err(err).Msg("failed to upgrade connection")
		return
	}

	// TODO: Extract user ID from auth token
	// userID := extractUserID(r)

	client := ws.NewClient(hub, conn, nil, logger)
	hub.Register() <- client

	go client.WritePump()
	go client.ReadPump()
}
