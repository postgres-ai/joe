/*
2019 © Postgres.ai
*/

package slackrtm

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/models"

	"github.com/pkg/errors"
	"github.com/slack-go/slack"
)

const errorNotPublished = "Message not published yet"

// Bot reactions.
const (
	ReactionRunning = "hourglass_flowing_sand"
	ReactionError   = "x"
	ReactionOK      = "white_check_mark"
)

// statusMapping defines a status-reaction map.
var statusMapping = map[models.MessageStatus]string{
	models.StatusRunning: ReactionRunning,
	models.StatusError:   ReactionError,
	models.StatusOK:      ReactionOK,
}

// Subtypes of incoming messages.
const (
	subtypeGeneral   = ""
	subtypeFileShare = "file_share"
)

// supportedSubtypes defines supported message subtypes.
var supportedSubtypes = []string{
	subtypeGeneral,
	subtypeFileShare,
}

// Messenger provides a communication via Slack API.
type Messenger struct {
	rtm    *slack.RTM
	config *SlackConfig
}

// NewMessenger creates a new Slack messenger service.
func NewMessenger(rtm *slack.RTM, cfg *SlackConfig) *Messenger {
	return &Messenger{
		rtm:    rtm,
		config: cfg,
	}
}

// Publish posts messages.
func (m *Messenger) Publish(message *models.Message) error {
	switch message.MessageType {
	case models.MessageTypeDefault:
		_, timestamp, err := m.rtm.PostMessage(message.ChannelID, slack.MsgOptionText(message.Text, false))
		if err != nil {
			return errors.Wrap(err, "failed to post a message")
		}

		message.MessageID = timestamp

	case models.MessageTypeThread:
		_, _, err := m.rtm.PostMessage(message.ChannelID, slack.MsgOptionText(message.Text, false),
			slack.MsgOptionTS(message.ThreadID))
		if err != nil {
			return errors.Wrap(err, "failed to post a thread message")
		}

	case models.MessageTypeEphemeral:
		timestamp, err := m.rtm.PostEphemeral(message.ChannelID, message.UserID, slack.MsgOptionText(message.Text, false))
		if err != nil {
			return errors.Wrap(err, "failed to post an ephemeral message")
		}

		message.MessageID = timestamp

	default:
		return errors.New("unknown message type")
	}

	return nil
}

// UpdateText updates a message text.
func (m *Messenger) UpdateText(message *models.Message) error {
	if !message.IsPublished() {
		return errors.New(errorNotPublished)
	}

	_, timestamp, _, err := m.rtm.UpdateMessage(message.ChannelID, message.MessageID, slack.MsgOptionText(message.Text, false))
	if err != nil {
		return errors.Wrap(err, "failed to update a message")
	}

	message.MessageID = timestamp

	return nil
}

// UpdateStatus updates message reactions.
func (m *Messenger) UpdateStatus(message *models.Message, status models.MessageStatus) error {
	if !message.IsPublished() {
		return errors.New(errorNotPublished)
	}

	if status == message.Status {
		return nil
	}

	reaction, ok := statusMapping[status]
	if !ok {
		return errors.Errorf("unknown status given: %s", status)
	}

	msgRef := slack.NewRefToMessage(message.ChannelID, message.MessageID)

	// Add new reaction.
	if err := m.rtm.AddReaction(reaction, msgRef); err != nil {
		message.SetStatus("")
		return err
	}

	// We have to add a new reaction before removing. In reverse order Slack UI will twitch.
	// TODO(anatoly): Remove reaction may fail, in that case we will lose data about added reaction.

	// Remove previous reaction.
	if oldReaction, ok := statusMapping[message.Status]; ok {
		if err := m.rtm.RemoveReaction(oldReaction, msgRef); err != nil {
			return err
		}
	}

	message.Status = status

	return nil
}

// Fail finishes the communication and marks message as failed.
func (m *Messenger) Fail(message *models.Message, text string) error {
	var err error

	errText := fmt.Sprintf("ERROR: %s", strings.TrimPrefix(text, "ERROR: "))

	if message.IsPublished() {
		message.AppendText(errText)
		err = m.UpdateText(message)
	} else {
		message.SetText(errText)
		err = m.Publish(message)
	}

	if err != nil {
		return err
	}

	if err := m.UpdateStatus(message, models.StatusError); err != nil {
		return errors.Wrap(err, "failed to update status")
	}

	if err := m.notifyAboutRequestFinish(message); err != nil {
		return errors.Wrap(err, "failed to notify about the request finish")
	}

	return nil
}

// OK finishes the communication and marks message as succeeding.
func (m *Messenger) OK(message *models.Message) error {
	if err := m.UpdateStatus(message, models.StatusOK); err != nil {
		return errors.Wrap(err, "failed to change reaction")
	}

	if err := m.notifyAboutRequestFinish(message); err != nil {
		return errors.Wrap(err, "failed to notify about finishing a long request")
	}

	return nil
}

// AddArtifact uploads artifacts to a communication channel.
func (m *Messenger) AddArtifact(title, explainResult, channelID, messageID string) (string, error) {
	filePlanWoExec, err := m.uploadFile(title, explainResult, channelID, messageID)
	if err != nil {
		log.Err("File upload failed:", err)
		return "", err
	}

	return filePlanWoExec.Permalink, nil
}

func (m *Messenger) uploadFile(title string, content string, channel string, ts string) (*slack.File, error) {
	const fileType = "txt"

	name := strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	filename := fmt.Sprintf("%s.%s", name, fileType)

	params := slack.UploadFileV2Parameters{
		Title:           title,
		Filename:        filename,
		FileSize:        len(content),
		Content:         content,
		Channel:         channel,
		ThreadTimestamp: ts,
	}

	fileSummary, err := m.rtm.UploadFileV2(params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to upload a file")
	}

	file, _, _, err := m.rtm.GetFileInfo(fileSummary.ID, 0, 0)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get an uploaded file info")
	}

	return file, nil
}

// DownloadArtifact downloads snippets from a communication channel.
func (m *Messenger) DownloadArtifact(privateURL string) ([]byte, error) {
	const (
		ContentTypeText     = "text/plain"
		HeaderAuthorization = "Authorization"
		HeaderContentType   = "Content-Type"
	)

	log.Dbg("Downloading snippet...")

	req, err := http.NewRequest(http.MethodGet, privateURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "cannot initialize a download snippet request")
	}

	req.Header.Set(HeaderAuthorization, fmt.Sprintf("Bearer %s", m.config.AccessToken))

	client := http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return nil, errors.Wrap(err, "cannot download snippet")
	}

	defer func() { _ = resp.Body.Close() }()

	snippet, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "cannot read the snippet content")
	}

	// In case of bad authorization Slack sends HTML page with auth form.
	// Snippet should have a plain text content type.
	contentType := resp.Header.Get(HeaderContentType)
	if resp.StatusCode == http.StatusUnauthorized || !strings.Contains(contentType, ContentTypeText) {
		return nil, errors.Errorf("unauthorized to download snippet: response code %d", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("cannot download snippet: response code %d", resp.StatusCode)
	}

	log.Dbg("Snippet downloaded.")

	return snippet, nil
}

func (m *Messenger) notifyAboutRequestFinish(message *models.Message) error {
	now := time.Now()
	if message.UserID == "" || now.Before(message.NotifyAt) {
		return nil
	}

	text := fmt.Sprintf("<@%s> :point_up_2:", message.UserID)

	if err := m.publishToThread(message, text); err != nil {
		return errors.Wrap(err, "failed to publish a user mention")
	}

	return nil
}

func (m *Messenger) publishToThread(message *models.Message, text string) error {
	threadMsg := &models.Message{
		MessageType: models.MessageTypeThread,
		ChannelID:   message.ChannelID,
		ThreadID:    message.MessageID,
		UserID:      message.UserID,
		Text:        text,
	}

	if err := m.Publish(threadMsg); err != nil {
		return errors.Wrap(err, "failed to publish a user mention")
	}

	return nil
}
