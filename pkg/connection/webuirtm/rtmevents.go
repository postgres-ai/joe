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

	// Incoming message types.
	pongType           = "pong"
	channelRequestType = "channels_request"
	messageType        = "message"
)

type pongData struct {
	replyID int
}

func processPong(event []byte) {
	pongType := pongData{}
	if err := json.Unmarshal(event, &pongType); err != nil {
		log.Dbg("failed unmarshal", err)
		return
	}

	log.Msg("pong received: %d", pongType.replyID)
}

func (a *Assistant) channels() {
	channels := []config.Channel{}

	work, ok := a.appCfg.ChannelMapping.CommunicationTypes[CommunicationType]

	wsEvent := WSEvent{
		Type: channelResponseType,
	}

	// For now, we will use only the first entry in the config.
	if !ok || len(work) == 0 {
		a.rtm.outgoingMessages <- wsEvent
		return
	}

	channels = append(channels, work[0].Channels...)

	data, err := json.Marshal(channels)
	if err != nil {
		log.Dbg("failed to unmarshal: %v", err.Error())
		a.rtm.outgoingMessages <- wsEvent
		return
	}

	wsEvent.Data = data
	a.rtm.outgoingMessages <- wsEvent
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
