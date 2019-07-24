/*
2019 © Anatoly Stansler anatoly@postgres.ai
2019 © Postgres.ai
*/

package chatapi

import (
	"fmt"
	"net/http"
	"io/ioutil"
	"strings"

	"../log"

	"github.com/nlopes/slack"
)

// TODO(anatoly): Refactor package to use as a full wrapper for nlopes/slack.

const CHAT_APPEND_SEPARATOR = "\n\n"

const ERROR_NOT_PUBLISHED = "Message not published yet"

const CONTENT_TYPE_TEXT = "text/plain"

type Chat struct {
	Api         *slack.Client
	AccessToken string
	VerificationToken string
}

type Message struct {
	ChannelId string
	Timestamp string // Used as message id in Slack API.
	Text      string // Used to accumulate message text to append new parts by edit.
	Reaction  string // We will support only one reaction for now.
	Chat      *Chat
}

func NewChat(accessToken string, verificationToken string) *Chat {
	chatApi := slack.New(accessToken)

	chat := Chat{
		Api: chatApi,
		AccessToken: accessToken,
		VerificationToken: verificationToken,
	}

	return &chat
}

func (c *Chat) NewMessage(channelId string) (*Message, error) {
	var msg Message

	if len(channelId) == 0 {
		return &msg, fmt.Errorf("Bad channelId specified")
	}

	msg = Message{
		ChannelId: channelId,
		Timestamp: "",
		Text:      "",
		Reaction:  "",
		Chat:      c,
	}

	return &msg, nil
}

func (c *Chat) DownloadSnippet(privateUrl string) ([]byte, error) {
	log.Dbg("Downloading snippet...")

	req, err := http.NewRequest("GET", privateUrl, nil)
	if err != nil {
		return []byte{}, fmt.Errorf("Cannot initialize download snippet request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.AccessToken))

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return []byte{}, fmt.Errorf("Cannot download snippet: %v", err)
	}
	defer resp.Body.Close()

	snippet, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []byte{}, fmt.Errorf("Cannot read snippet contents: %v", err)
	}

	// In case of bad authorization Slack sends HTML page with auth form.
	// Snippet should have a plain text content type.
	contentType := resp.Header.Get("Content-Type")
	isText := strings.Contains(contentType, CONTENT_TYPE_TEXT)
	if resp.StatusCode == http.StatusUnauthorized || !isText {
		return []byte{}, fmt.Errorf("Unauthorized to download snippet")
	}

	if resp.StatusCode != http.StatusOK {
		return []byte{}, fmt.Errorf("Cannot download snippet: response code %d",
			resp.StatusCode)
	}

	log.Dbg("Snippet downloaded.")

	return snippet, nil
}

// TODO(anatoly): Retries.
// Publish a message.
func (m *Message) Publish(text string) error {
	channelId, timestamp, err := m.Chat.Api.PostMessage(m.ChannelId,
		slack.MsgOptionText(text, false))
	if err != nil {
		return err
	}

	m.ChannelId = channelId // Shouldn't change, but update just in case.
	m.Timestamp = timestamp
	m.Text = text

	return nil
}

// Append text to a published message.
// Slack: User will not get notification. Publish a new message if notification needed.
func (m *Message) Append(text string) error {
	if !m.isPublished() {
		return fmt.Errorf(ERROR_NOT_PUBLISHED)
	}

	newText := m.Text + CHAT_APPEND_SEPARATOR + text

	channelId, timestamp, _, err := m.Chat.Api.UpdateMessage(m.ChannelId,
		m.Timestamp, slack.MsgOptionText(newText, false))
	if err != nil {
		return err
	}

	m.ChannelId = channelId // Shouldn't change, but update just in case.
	m.Timestamp = timestamp
	m.Text = newText

	return nil
}

// Remove previous reactions (from bot) in a published message and add a new one.
func (m *Message) ChangeReaction(reaction string) error {
	if !m.isPublished() {
		return fmt.Errorf(ERROR_NOT_PUBLISHED)
	}

	if reaction == m.Reaction {
		return nil
	}

	msgRef := slack.NewRefToMessage(m.ChannelId, m.Timestamp)

	// Add new reaction.
	err := m.Chat.Api.AddReaction(reaction, msgRef)
	if err != nil {
		m.Reaction = ""
		return err
	}

	// We have to add a new reaction before removing. In reverse order Slack UI will twitch.
	// TODO(anatoly): Remove reaction may fail, in that case we will lose data about added reaction.

	// Remove previous reaction.
	if len(m.Reaction) != 0 {
		err := m.Chat.Api.RemoveReaction(m.Reaction, msgRef)
		if err != nil {
			return err
		}
	}

	m.Reaction = reaction

	return nil
}

func (m *Message) isPublished() bool {
	if len(m.ChannelId) == 0 || len(m.Timestamp) == 0 {
		return false
	}

	return true
}
