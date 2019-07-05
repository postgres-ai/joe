/*
2019 © Anatoly Stansler anatoly@postgres.ai
2019 © Postgres.ai
*/

package chat

import (
	"fmt"

	"github.com/nlopes/slack"
)

// TODO(anatoly): Refactor package to use as a full wrapper for nlopes/slack.

const CHAT_APPEND_SEPARATOR = "\n\n"

const ERROR_NOT_PUBLISHED = "Message not published yet"

type Message struct {
	ChannelId string
	Timestamp string // Used as message id in Slack API.
	Text      string // Used to accumulate message text to append new parts by edit.
	Reaction  string // We will support only one reaction for now.
	ChatApi   *slack.Client
}

func NewMessage(channelId string, chatApi *slack.Client) (*Message, error) {
	var msg Message

	if len(channelId) == 0 {
		return &msg, fmt.Errorf("Bad channelId specified")
	}

	if chatApi == nil {
		return &msg, fmt.Errorf("Bad chatApi specified")
	}

	msg = Message{
		ChannelId: channelId,
		Timestamp: "",
		Text:      "",
		Reaction:  "",
		ChatApi:   chatApi,
	}

	return &msg, nil
}

// TODO(anatoly): Retries.
// Publish a message.
func (m *Message) Publish(text string) error {
	channelId, timestamp, err := m.ChatApi.PostMessage(m.ChannelId,
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

	channelId, timestamp, _, err := m.ChatApi.UpdateMessage(m.ChannelId, m.Timestamp,
		slack.MsgOptionText(newText, false))
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
	err := m.ChatApi.AddReaction(reaction, msgRef)
	if err != nil {
		m.Reaction = ""
		return err
	}

	// We have to add a new reaction before removing. In reverse order Slack UI will twitch.
	// TODO(anatoly): Remove reaction may fail, in that case we will lose data about added reaction.

	// Remove previous reaction.
	if len(m.Reaction) != 0 {
		err := m.ChatApi.RemoveReaction(m.Reaction, msgRef)
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
