/*
2019 © Postgres.ai
*/

package slack

import (
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/util"
)

// MessageValidator validates incoming messages.
type MessageValidator struct {
}

// Validate validates an incoming message.
func (mv MessageValidator) Validate(incomingMessage *models.IncomingMessage) error {
	if incomingMessage == nil {
		return errors.New("input event must not be nil")
	}

	// Skip messages sent by bots.
	if incomingMessage.UserID == "" {
		return errors.New("userID must not be empty")
	}

	// Skip messages from threads.
	if incomingMessage.ThreadID != "" {
		return errors.New("skip message in thread")
	}

	if !util.Contains(supportedSubtypes, incomingMessage.SubType) {
		return errors.Errorf("subtype %q is not supported", incomingMessage.SubType)
	}

	if incomingMessage.ChannelID == "" {
		return errors.New("bad channelID specified")
	}

	return nil
}
