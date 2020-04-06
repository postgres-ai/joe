package webui

import (
	"errors"

	"gitlab.com/postgres-ai/joe/pkg/models"
)

// MessageValidator validates incoming messages.
type MessageValidator struct {
}

// Validate validates an incoming message.
func (m MessageValidator) Validate(incomingMessage *models.IncomingMessage) error {
	if incomingMessage == nil {
		return errors.New("input event must not be nil")
	}

	// Skip messages sent by bots.
	if incomingMessage.UserID == "" {
		return errors.New("userID must not be empty")
	}

	if incomingMessage.ChannelID == "" {
		return errors.New("bad channelID specified")
	}

	if incomingMessage.SessionID == "" {
		return errors.New("bad sessionID specified")
	}

	if incomingMessage.CommandID == "" {
		return errors.New("bad commandID specified")
	}

	return nil
}
