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
	TechnicalEvent   chan RTMEvent
	outgoingMessages chan WSEvent
}

// NewRTM creates a new RTM client.
func NewRTM() *RTM {
	return &RTM{
		mu:               &sync.Mutex{},
		forcePing:        make(chan struct{}),
		closeConnection:  make(chan struct{}),
		stop:             make(chan struct{}),
		IncomingMessages: make(chan json.RawMessage, defaultIncomingMessageChannelSize),
		TechnicalEvent:   make(chan RTMEvent, defaultInternalEventChannelSize),
		pingInterval:     defaultPingInterval,
		outgoingMessages: make(chan WSEvent, defaultIncomingMessageChannelSize),
	}
}

type wsConfig struct {
	url   string
	token string
}

// WSEvent represents incoming messages.
type WSEvent struct {
	Type string          `json:"type,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// RTMEvent represents internal events.
type RTMEvent struct {
	Type string
	Data fmt.Stringer
}

// String prints RTMEvent.
func (e RTMEvent) String() string {
	return fmt.Sprintf("Type: %s, Data: %s", e.Type, e.Data)
}

// InfoEvent represents an internal info event.
type InfoEvent struct {
	Message string
}

// String prints the message of InfoEvent.
func (i InfoEvent) String() string {
	return i.Message
}

// ManageConnection manages a web-socket connection.
func (rtm *RTM) ManageConnection(ctx context.Context) {
	const maxSleepInterval = 60
	const multiplier = 2
	const maxAttempts = 100

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

		rtm.TechnicalEvent <- RTMEvent{Type: "connected", Data: InfoEvent{Message: rtm.config.url}}

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


		// listen for messages that need to be sent
		case msg := <-rtm.outgoingMessages:
			rtm.sendOutgoingMessage(msg)
		}
	}
}

func (rtm *RTM) ping() error {
	if rtm.conn == nil {
		return errors.New("connection is not initialized")
	}

	rtm.TechnicalEvent <- RTMEvent{Type: "ping", Data: InfoEvent{Message: "ping event"}}

	return rtm.conn.PingHandler()(`ping`)
}

func (rtm *RTM) stopRTM() {
	close(rtm.stop)
}

func (rtm *RTM) sendOutgoingMessage(_ WSEvent) {
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
