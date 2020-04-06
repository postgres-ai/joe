/*
2019 © Postgres.ai
*/

package webui

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
)

// Messenger provides a communication via Platform API.
type Messenger struct {
	api *platform.Client
}

// NewMessenger creates a new Platform messenger service.
func NewMessenger(api *platform.Client) *Messenger {
	return &Messenger{
		api: api,
	}
}

func (m Messenger) postMessage(ctx context.Context, message *models.Message) error {
	postMessage := platform.PostMessage{
		CommandID: message.CommandID,
		MessageID: message.MessageID,
		Text:      message.Text,
		Status:    string(message.Status),
		SessionID: message.SessionID,
	}

	messageID, err := m.api.PostMessage(ctx, postMessage)
	if err != nil {
		return errors.Wrap(err, "failed to post a message to Platform")
	}

	if message.MessageID == "" {
		message.MessageID = messageID
	}

	return nil
}

// Publish posts messages.
func (m Messenger) Publish(message *models.Message) error {
	return m.postMessage(context.TODO(), message)
}

// UpdateText updates a message text.
func (m Messenger) UpdateText(message *models.Message) error {
	return m.postMessage(context.TODO(), message)
}

// UpdateStatus updates message status.
func (m Messenger) UpdateStatus(message *models.Message, status models.MessageStatus) error {
	message.SetStatus(status)

	return m.postMessage(context.TODO(), message)
}

// Fail finishes the communication and marks message as failed.
func (m Messenger) Fail(message *models.Message, text string) error {
	errText := fmt.Sprintf("ERROR: %s", text)

	if message.IsPublished() {
		message.AppendText(errText)
	} else {
		message.SetText(errText)
	}

	message.SetStatus(models.StatusError)

	return m.postMessage(context.TODO(), message)
}

// OK finishes the communication and marks message as succeeding.
func (m Messenger) OK(message *models.Message) error {
	message.SetStatus(models.StatusOK)

	return m.postMessage(context.TODO(), message)
}

// AddArtifact uploads artifacts to a channel.
func (m Messenger) AddArtifact(title, content, _, messageID string) (artifactLink string, err error) {
	artifact := platform.ArtifactUploadParameters{
		MessageID: messageID,
		Title:     title,
		Content:   content,
	}

	return m.api.AddArtifact(context.TODO(), artifact)
}

// DownloadArtifact downloads snippets from a communication channel.
func (m Messenger) DownloadArtifact(artifactURL string) (response []byte, err error) {
	panic("artifact downloading is not supported")
}
