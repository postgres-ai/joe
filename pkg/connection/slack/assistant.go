/*
2019 © Postgres.ai
*/

// Package slack provides the Slack implementation of the communication interface.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"

	"github.com/nlopes/slack"
	"github.com/nlopes/slack/slackevents"
	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/dblab"
	"gitlab.com/postgres-ai/joe/pkg/services/msgproc"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

// Assistant provides a service for interaction with a communication channel.
type Assistant struct {
	credentialsCfg *config.Credentials
	procMu         sync.RWMutex
	msgProcessors  map[string]connection.MessageProcessor
	prefix         string
	appCfg         *config.Config
}

// SlackConfig defines a slack configuration parameters.
type SlackConfig struct {
	AccessToken   string
	SigningSecret string
}

// NewAssistant returns a new assistant service.
func NewAssistant(cfg *config.Credentials, appCfg *config.Config) (*Assistant, error) {
	if err := validateCredentials(cfg); err != nil {
		return nil, errors.Wrap(err, "invalid credentials given")
	}

	assistant := &Assistant{
		credentialsCfg: cfg,
		appCfg:         appCfg,
		msgProcessors:  make(map[string]connection.MessageProcessor),
	}

	return assistant, nil
}

func validateCredentials(credentials *config.Credentials) error {
	if credentials == nil || credentials.AccessToken == "" || credentials.SigningSecret == "" {
		return errors.New(`"accessToken" and "signingSecret" must not be empty`)
	}

	return nil
}

// Init registers assistant handlers.
func (a *Assistant) Init() error {
	log.Dbg("URL-path prefix: ", a.prefix)

	if a.lenMessageProcessor() == 0 {
		return errors.New("no message processor set")
	}

	for path, handleFunc := range a.handlers() {
		http.Handle(fmt.Sprintf("%s/%s", a.prefix, path), handleFunc)
	}

	return nil
}

func (a *Assistant) lenMessageProcessor() int {
	a.procMu.RLock()
	defer a.procMu.RUnlock()

	return len(a.msgProcessors)
}

// SetHandlerPrefix prepares and sets a handler URL-path prefix.
func (a *Assistant) SetHandlerPrefix(prefix string) {
	a.prefix = fmt.Sprintf("/%s", strings.Trim(prefix, "/"))
}

// AddDBLabInstanceForChannel sets a message processor for a specific channel.
func (a *Assistant) AddDBLabInstanceForChannel(channelID string, dbLabInstance *dblab.Instance) {
	messageProcessor := a.buildMessageProcessor(a.appCfg, dbLabInstance)

	a.procMu.Lock()
	a.msgProcessors[channelID] = messageProcessor
	a.procMu.Unlock()
}

func (a *Assistant) buildMessageProcessor(appCfg *config.Config, dbLabInstance *dblab.Instance) *msgproc.ProcessingService {
	slackCfg := &SlackConfig{
		AccessToken:   a.credentialsCfg.AccessToken,
		SigningSecret: a.credentialsCfg.SigningSecret,
	}

	chatAPI := slack.New(slackCfg.AccessToken)

	messenger := NewMessenger(chatAPI, slackCfg)
	userInformer := NewUserInformer(chatAPI)
	userManager := usermanager.NewUserManager(userInformer, appCfg.Quota)

	processingCfg := msgproc.ProcessingConfig{
		App:      appCfg.App,
		Platform: appCfg.Platform,
		Explain:  appCfg.Explain,
		DBLab:    dbLabInstance.Config(),
	}

	return msgproc.NewProcessingService(messenger, MessageValidator{}, dbLabInstance.Client(), userManager, processingCfg)
}

// getProcessingService returns processing service by channelID.
func (a *Assistant) getProcessingService(channelID string) (connection.MessageProcessor, error) {
	a.procMu.RLock()
	defer a.procMu.RUnlock()

	messageProcessor, ok := a.msgProcessors[channelID]
	if !ok {
		return nil, errors.Errorf("message processor for %q channel not found", channelID)
	}

	return messageProcessor, nil
}

// CheckIdleSessions check the running user sessions for idleness.
func (a *Assistant) CheckIdleSessions(ctx context.Context) {
	a.procMu.RLock()
	for _, proc := range a.msgProcessors {
		proc.CheckIdleSessions(ctx)
	}
	a.procMu.RUnlock()
}

func (a *Assistant) handlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"": a.handleEvent,
	}
}

func (a *Assistant) handleEvent(w http.ResponseWriter, r *http.Request) {
	log.Msg("Request received:", html.EscapeString(r.URL.Path))

	// TODO(anatoly): Respond time according to Slack API timeouts policy.
	// Slack sends retries in case of timedout responses.
	if r.Header.Get("X-Slack-Retry-Num") != "" {
		log.Dbg("Message filtered: Slack Retry")
		return
	}

	if err := a.verifyRequest(r); err != nil {
		log.Dbg("Message filtered: Verification failed:", err.Error())
		w.WriteHeader(http.StatusForbidden)

		return
	}

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(r.Body); err != nil {
		log.Err("Failed to read the request body:", err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	body := buf.Bytes()

	eventsAPIEvent, err := a.parseEvent(body)
	if err != nil {
		log.Err("Event parse error:", err)
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	// TODO (akartasov): event processing function.
	switch eventsAPIEvent.Type {
	// Used to verify bot's API URL for Slack.
	case slackevents.URLVerification:
		log.Dbg("Event type: URL verification")

		var r *slackevents.ChallengeResponse

		err := json.Unmarshal(body, &r)
		if err != nil {
			log.Err("Challenge parse error:", err)
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "text")
		w.Write([]byte(r.Challenge))

	// General Slack events.
	case slackevents.CallbackEvent:
		switch ev := eventsAPIEvent.InnerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			log.Dbg("Event type: AppMention")

			msgProcessor, err := a.getProcessingService(ev.Channel)
			if err != nil {
				log.Err("failed to get processing service", err)
				return
			}

			msg := a.appMentionEventToIncomingMessage(ev)
			msgProcessor.ProcessAppMentionEvent(msg)

		case *slackevents.MessageEvent:
			log.Dbg("Event type: Message")

			if ev.BotID != "" {
				// Skip messages sent by bots.
				return
			}

			msgProcessor, err := a.getProcessingService(ev.Channel)
			if err != nil {
				log.Err("failed to get processing service", err)
				return
			}

			msg := a.messageEventToIncomingMessage(ev)
			msgProcessor.ProcessMessageEvent(msg)

		default:
			log.Dbg("Event filtered: Inner event type not supported")
		}

	default:
		log.Dbg("Event filtered: Event type not supported")
	}
}

// appMentionEventToIncomingMessage converts a Slack application mention event to the standard incoming message.
func (a *Assistant) appMentionEventToIncomingMessage(event *slackevents.AppMentionEvent) models.IncomingMessage {
	inputEvent := models.IncomingMessage{
		Text:      event.Text,
		ChannelID: event.Channel,
		UserID:    event.User,
		Timestamp: event.TimeStamp,
		ThreadID:  event.ThreadTimeStamp,
	}

	return inputEvent
}

// messageEventToIncomingMessage converts a Slack message event to the standard incoming message.
func (a *Assistant) messageEventToIncomingMessage(event *slackevents.MessageEvent) models.IncomingMessage {
	inputEvent := models.IncomingMessage{
		SubType:     event.SubType,
		Text:        event.Text,
		ChannelID:   event.Channel,
		ChannelType: event.ChannelType,
		UserID:      event.User,
		Timestamp:   event.TimeStamp,
		ThreadID:    event.ThreadTimeStamp,
	}

	// Skip messages sent by bots.
	if event.BotID != "" {
		inputEvent.UserID = ""
	}

	files := event.Files
	if len(files) > 0 {
		inputEvent.SnippetURL = files[0].URLPrivate
	}

	return inputEvent
}

// parseEvent parses slack events.
func (a *Assistant) parseEvent(rawEvent []byte) (slackevents.EventsAPIEvent, error) {
	return slackevents.ParseEvent(rawEvent, slackevents.OptionNoVerifyToken())
}

// verifyRequest verifies a request coming from Slack
func (a *Assistant) verifyRequest(r *http.Request) error {
	secretsVerifier, err := slack.NewSecretsVerifier(r.Header, a.credentialsCfg.SigningSecret)
	if err != nil {
		return errors.Wrap(err, "failed to init the secrets verifier")
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return errors.Wrap(err, "failed to read the request body")
	}

	// Set a body with the same data we read.
	r.Body = ioutil.NopCloser(bytes.NewBuffer(body))

	if _, err := secretsVerifier.Write(body); err != nil {
		return errors.Wrap(err, "failed to prepare the request body")
	}

	if err := secretsVerifier.Ensure(); err != nil {
		return errors.Wrap(err, "failed to ensure a secret token")
	}

	return nil
}
