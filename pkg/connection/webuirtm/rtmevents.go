package webuirtm

import (
	"context"
	"encoding/json"

	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/config"
)

const (
	// Outgoing message types.
	pingType            = "ping"
	channelResponseType = "channels_response"
	errorType           = "error"

	// Incoming message types.
	pongType           = "pong"
	channelRequestType = "channels_request"
	messageType        = "message"
)

// wsEvent represents incoming messages.
type wsEvent struct {
	Type string          `json:"type,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

type pingData struct {
	ID        int   `json:"id"`
	Timestamp int64 `json:"timestamp"`
}

type pongData struct {
	ReplyID int `json:"reply_id"`
}

type channelRequest struct {
	RequestID string `json:"request_id"`
}

type techResponseData struct {
	Message string `json:"message"`
}

type channelResponseData struct {
	Channels []config.Channel `json:"channels"`
}

func processPong(event []byte) {
	pongType := pongData{}
	if err := json.Unmarshal(event, &pongType); err != nil {
		log.Dbg("failed unmarshal", err)
		return
	}

	log.Msg("pong received: %d", pongType.ReplyID)
}

func (a *Assistant) sendAvailableChannels(event []byte) {
	channelRequest := channelRequest{}

	if err := json.Unmarshal(event, &channelRequest); err != nil {
		log.Dbg("failed to unmarshal request: %v", err.Error())
		a.rtm.outgoingMessages <- outgoingEvent{
			Type: errorType,
			Data: techResponseData{
				Message: err.Error(),
			},
		}

		return
	}

	if channelRequest.RequestID == "" {
		log.Dbg("requestID must not be empty")
		a.rtm.outgoingMessages <- outgoingEvent{
			Type: errorType,
			Data: techResponseData{Message: "requestID must not be empty"},
		}

		return
	}

	a.rtm.outgoingMessages <- a.getAvailableChannels(channelRequest.RequestID)
}

func (a *Assistant) getAvailableChannels(requestID string) outgoingEvent {
	channelResponse := channelResponseData{
		Channels: []config.Channel{},
	}

	outgoingEvent := outgoingEvent{
		Type:      channelResponseType,
		RequestID: requestID,
		Data:      channelResponse,
	}

	work, ok := a.appCfg.ChannelMapping.CommunicationTypes[CommunicationType]
	if !ok || len(work) == 0 {
		return outgoingEvent
	}

	channelResponse.Channels = append(channelResponse.Channels, work[0].Channels...)
	outgoingEvent.Data = channelResponse

	return outgoingEvent
}

func (a *Assistant) processMessage(event []byte) {
	webMessage := Message{}
	if err := json.Unmarshal(event, &webMessage); err != nil {
		log.Dbg("failed unmarshal", err)
		return
	}

	log.Msg("Message: %d", webMessage.ChannelID)

	svc, err := a.getProcessingService(webMessage.ChannelID)
	if err != nil {
		log.Err("Failed to get a processing service", err)
		return
	}

	go svc.ProcessMessageEvent(context.TODO(), webMessage.ToIncomingMessage())
}
