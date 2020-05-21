package webuirtm

import (
	"context"
	"encoding/json"
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
	IncomingMessages chan json.RawMessage
	TechnicalEvent   chan RTMEvent
	outgoingMessages chan RTMEvent
}

// NewRTM creates a new RTM client.
func NewRTM() *RTM {
	return &RTM{
		IncomingMessages: make(chan json.RawMessage, defaultIncomingMessageChannelSize),
		TechnicalEvent:   make(chan RTMEvent, defaultInternalEventChannelSize),
		pingInterval:     defaultPingInterval,
	}
}

type wsConfig struct {
	url   string
	token string
}

// RTMessage represents incoming messages.
type RTMessage struct {
	Type string          `json:"type,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// RTMEvent represents internal events.
type RTMEvent struct {
	Type string
	Data json.RawMessage
}

// ManageConnection manages a web-socket connection.
func (rtm *RTM) ManageConnection(ctx context.Context) {
	const maxSleepInterval = 60
	const multiplier = 2

	for attempts := 1; ; attempts++ {
		if err := rtm.connect(ctx); err != nil {
			// TODO: check auth errors.

			sleepInterval := attempts * multiplier
			if sleepInterval > maxSleepInterval {
				sleepInterval = maxSleepInterval
			}

			time.Sleep(time.Duration(sleepInterval) * time.Second)
			continue
		}

		go rtm.handleEvents(ctx)

		return
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
		case <-ctx.Done():
			return

		case <-ticker.C:
			if err := rtm.ping(); err != nil {
				_ = rtm.reconnect()
				return
			}

		// listen for messages that need to be sent
		case msg := <-rtm.outgoingMessages:
			rtm.sendOutgoingMessage(msg)
		}
	}
}

func (rtm *RTM) ping() error {

	return nil
}

func (rtm *RTM) reconnect() error {
	return nil
}

func (rtm *RTM) sendOutgoingMessage(_ RTMEvent) {
}

// Disconnect performs disconnection.
func (rtm *RTM) Disconnect() error {
	log.Dbg("disconnecting ", rtm.config.url)

	// TODO: fix
	err := rtm.conn.WriteControl(
		websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	time.Sleep(time.Second)

	if rtm.conn != nil {
		err = rtm.conn.Close()
	}

	rtm.mu.Lock()
	rtm.conn = nil
	rtm.mu.Unlock()

	return err
}

func (rtm *RTM) handleIncomingMessages() {
	for {
		if err := rtm.receiveIncomingMessage(); err != nil {
			// TODO: reconnect.
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
			//select {
			//case rtm.forcePing <- true:
			//case <-rtm.disconnected:
			//}

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
		//case <-rtm.disconnected:
		//	rtm.Debugln("disonnected while attempting to send raw rawMessage")
	}

	return nil
}

// Close closes a connection.
func (rtm *RTM) Close() {

}
