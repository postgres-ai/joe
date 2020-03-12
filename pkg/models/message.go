/*
2019 © Postgres.ai
*/

package models

import (
	"errors"
	"time"
)

// ChatAppendSeparator separates appended part of a message.
const ChatAppendSeparator = "\n\n"

// Message types of published messages.
const (
	MessageTypeDefault = iota
	MessageTypeThread
	MessageTypeEphemeral
)

// Message status.
const (
	StatusRunning = "running"
	StatusError   = "error"
	StatusOK      = "ok"
)

// IncomingMessage defines a standard representation of incoming events.
type IncomingMessage struct {
	SubType     string
	Text        string
	SnippetURL  string
	ChannelID   string
	ChannelType string
	UserID      string
	Timestamp   string
	ThreadID    string
}

// Message struct defines an output message.
type Message struct {
	MessageID   string
	MessageType int
	Status      MessageStatus
	ChannelID   string
	ThreadID    string
	UserID      string
	Text        string
	CreatedAt   time.Time
	NotifyAt    time.Time
}

// MessageStatus defines status of a message.
type MessageStatus string

// NewMessage creates a new message.
func NewMessage(channelID string) *Message {
	return &Message{
		ChannelID: channelID,
		CreatedAt: time.Now(),
	}
}

// SetText sets text to the message.
func (m *Message) SetText(text string) {
	m.Text = text
}

// AppendText appends a new string to the current text throw a chat append separator.
func (m *Message) AppendText(text string) {
	m.Text = m.Text + ChatAppendSeparator + text
}

// SetMessageType sets a message type.
func (m *Message) SetMessageType(messageType int) {
	m.MessageType = messageType
}

// SetStatus sets message status.
func (m *Message) SetStatus(status MessageStatus) {
	m.Status = status
}

// SetUserID sets a user ID of the message.
func (m *Message) SetUserID(userID string) {
	m.UserID = userID
}

// SetNotifyAt sets timestamp to notify a user about the finish of a long query.
func (m *Message) SetNotifyAt(notificationTimeout time.Duration) error {
	if m.CreatedAt.IsZero() {
		return errors.New("createdAt timestamp required")
	}

	m.NotifyAt = m.CreatedAt.Add(notificationTimeout)

	return nil
}

// IsPublished checks if the message is already published.
func (m *Message) IsPublished() bool {
	return m.ChannelID != "" && m.MessageID != ""
}
