/*
2019 © Postgres.ai
*/

// Package connection represents communication channels.
package connection

import (
	"gitlab.com/postgres-ai/joe/pkg/models"
)

// Messenger defines the interface for communication with an assistant.
type Messenger interface {
	// Publish describes a method for posting of various type messages.
	Publish(message *models.Message) error

	// UpdateText describes a method for updating a message text.
	UpdateText(message *models.Message) error

	// UpdateStatus describes a method for changing a message status.
	UpdateStatus(message *models.Message, status models.MessageStatus) error

	MessageFinalizer
	ArtifactLoader
}

// MessageValidator defines the interface for message validation.
type MessageValidator interface {
	// Validate validates an incoming message.
	Validate(inputEvent *models.IncomingMessage) error
}

// MessageFinalizer finishes a message processing.
type MessageFinalizer interface {
	// TODO(akartasov): Group Fail and OK methods to Finish(message *structs.Message) error
	// Fail finishes a message processing and marks a message as failed.
	Fail(message *models.Message, text string) error

	// OK finishes a message processing and marks a message as succeeding.
	OK(message *models.Message) error
}

// MessageValidator defines the interface for artifacts management.
type ArtifactLoader interface {
	// AddArtifact describes a method for an uploading message artifacts to a communication channel.
	AddArtifact(name string, result string, channelID string, messageID string) (artifactLink string, err error)

	// DownloadArtifact describes a method for a downloading message artifacts and snippets from a communication channel.
	DownloadArtifact(artifactURL string) (response []byte, err error)
}
