package webuirtm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"gitlab.com/postgres-ai/database-lab/pkg/log"
)

const (
	defaultPingInterval               = 30 * time.Second
	defaultIncomingMessageChannelSize = 20
	defaultInternalEventChannelSize   = 10
)

// RTM provides a real-time message client for Web UI.
type RTM struct {
	config           wsConfig
	pingInterval     time.Duration
	mu               *sync.Mutex
	conn             *websocket.Conn
	forcePing        chan struct{}
	closeConnection  chan struct{}
	stop             chan struct{}
	IncomingMessages chan json.RawMessage
	TechnicalEvent   chan infoEvent
	outgoingMessages chan outgoingEvent
}

// NewRTM creates a new RTM client.
func NewRTM() *RTM {
	return &RTM{
		mu:               &sync.Mutex{},
		forcePing:        make(chan struct{}),
		closeConnection:  make(chan struct{}),
		stop:             make(chan struct{}),
		IncomingMessages: make(chan json.RawMessage, defaultIncomingMessageChannelSize),
		TechnicalEvent:   make(chan infoEvent, defaultInternalEventChannelSize),
		pingInterval:     defaultPingInterval,
		outgoingMessages: make(chan outgoingEvent, defaultIncomingMessageChannelSize),
	}
}

type wsConfig struct {
	url string
}

type outgoingEvent struct {
	Type      string      `json:"type"`
	RequestID string      `json:"request_id"`
	Data      interface{} `json:"data,omitempty"`
}

// infoEvent represents internal events.
type infoEvent struct {
	Type    string
	Message string
}

// String prints infoEvent.
func (e infoEvent) String() string {
	return fmt.Sprintf("Type: %s, Message: %s", e.Type, e.Message)
}

// ManageConnection manages a web-socket connection.
func (rtm *RTM) ManageConnection(ctx context.Context) {
	// TODO: move to config.
	const (
		maxSleepInterval = 60
		multiplier       = 2
		maxAttempts      = 100
	)

	var attempts = 0

	defer rtm.stopRTM()

	for {
		if err := rtm.connect(ctx); err != nil {
			log.Dbg(fmt.Sprintf("%#v\n", err))

			switch {
			case err == websocket.ErrBadHandshake:
				rtm.disconnect(err)
				return

			case attempts > maxAttempts:
				rtm.disconnect(errors.New("the limit of connection attempts is exceed"))
				return
			}

			attempts++

			sleepInterval := attempts * multiplier
			if sleepInterval > maxSleepInterval {
				sleepInterval = maxSleepInterval
			}

			time.Sleep(time.Duration(sleepInterval) * time.Second)

			continue
		}

		rtm.TechnicalEvent <- infoEvent{Type: "connected", Message: rtm.config.url}

		// Reset attempts after successful connection.
		attempts = 0

		go rtm.listenIncomingMessages()

		rtm.handleEvents(ctx)

		select {
		case <-rtm.stop:
			rtm.disconnect(errors.New("stop signal on manage connection"))
			return
		default:
		}
	}
}

// connect initializes connection.
func (rtm *RTM) connect(ctx context.Context) error {
	log.Dbg("connecting to ", rtm.config.url)

	c, _, err := websocket.DefaultDialer.DialContext(ctx, rtm.config.url, nil)
	if err != nil {
		return err
	}

	rtm.mu.Lock()
	rtm.conn = c
	rtm.mu.Unlock()

	return nil
}

func (rtm *RTM) handleEvents(ctx context.Context) {
	ticker := time.NewTicker(rtm.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rtm.closeConnection:
			rtm.disconnect(errors.New("close connection signal"))
			return

		case <-ctx.Done():
			return

		case <-rtm.forcePing:
			if err := rtm.ping(); err != nil {
				rtm.disconnect(err)
				return
			}

		case <-ticker.C:
			if err := rtm.ping(); err != nil {
				rtm.disconnect(err)
				return
			}

		case msg := <-rtm.TechnicalEvent:
			log.Msg(msg.String())

		// Listen for messages that need to be sent.
		case msg := <-rtm.outgoingMessages:
			rtm.sendOutgoingMessage(msg)
		}
	}
}

func (rtm *RTM) ping() error {
	if rtm.conn == nil {
		return errors.New("connection is not initialized")
	}

	rtm.TechnicalEvent <- infoEvent{Type: pingType, Message: "ping event"}

	pingEvent := outgoingEvent{
		Type: pingType,
		Data: pingData{
			ID:        0, // TODO: increment
			Timestamp: time.Now().UTC().Unix(),
		},
	}

	pingBytes, err := json.Marshal(pingEvent)
	if err != nil {
		return err
	}

	return rtm.conn.PingHandler()(string(pingBytes))
}

func (rtm *RTM) stopRTM() {
	close(rtm.stop)
}

func (rtm *RTM) sendOutgoingMessage(_ outgoingEvent) {
}

// disconnect performs disconnection.
func (rtm *RTM) disconnect(cause error) {
	log.Dbg("disconnecting ", rtm.config.url, cause)

	if rtm.conn != nil {
		if err := rtm.conn.Close(); err != nil {
			log.Err("Failed to disconnect: ", err)
		}
	}

	rtm.mu.Lock()
	rtm.conn = nil
	rtm.mu.Unlock()
}

func (rtm *RTM) listenIncomingMessages() {
	for {
		if err := rtm.receiveIncomingMessage(); err != nil {
			select {
			case rtm.closeConnection <- struct{}{}:
			case <-rtm.stop:
			}

			return
		}
	}
}

func (rtm *RTM) receiveIncomingMessage() error {
	rawMessage := json.RawMessage{}
	err := rtm.conn.ReadJSON(&rawMessage)

	if err != nil {
		switch {
		case websocket.IsUnexpectedCloseError(err):
			// Check if the connection was closed.
			return err

		case err == io.ErrUnexpectedEOF:
			// Trigger a 'PING' to detect potential websocket disconnect.
			select {
			case rtm.forcePing <- struct{}{}:
			case <-rtm.stop:
			}

			return nil

		default:
			// Send event to TechnicalEvent.
			return err
		}
	}

	if len(rawMessage) == 0 {
		log.Dbg("Received empty RawMessage")
		return nil
	}

	log.Dbg("Incoming Event:", string(rawMessage))

	select {
	case rtm.IncomingMessages <- rawMessage:
	case <-rtm.stop:
		log.Dbg("disconnected while attempting to send rawMessage")
	}

	return nil
}
