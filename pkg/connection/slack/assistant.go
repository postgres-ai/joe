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
	"regexp"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/features"
	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/dblab"
	"gitlab.com/postgres-ai/joe/pkg/services/msgproc"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

var (
	linkRegexp  = regexp.MustCompile(`<http:\/\/[\w.]+\|([.\w]+)>`)
	emailRegexp = regexp.MustCompile(`<mailto:['@\w.]+\|(['@.\w]+)>`)
)

// CommunicationType defines a workspace type.
const CommunicationType = "slack"

// Assistant provides a service for interaction with a communication channel.
type Assistant struct {
	credentialsCfg *config.Credentials
	procMu         sync.RWMutex
	msgProcessors  map[string]connection.MessageProcessor
	prefix         string
	appCfg         *config.Config
	featurePack    *features.Pack
	messenger      *Messenger
	userManager    *usermanager.UserManager
	platformClient *platform.Client
}

// NewAssistant returns a new assistant service.
func NewAssistant(cfg *config.Credentials, appCfg *config.Config, handlerPrefix string, pack *features.Pack) (*Assistant, error) {
	prefix := fmt.Sprintf("/%s", strings.Trim(handlerPrefix, "/"))

	chatAPI := slack.New(cfg.AccessToken)
	messenger := NewMessenger(chatAPI, &MessengerConfig{AccessToken: cfg.AccessToken})
	userInformer := NewUserInformer(chatAPI)
	userManager := usermanager.NewUserManager(userInformer, appCfg.Enterprise.Quota)

	platformClient, err := platform.NewClient(appCfg.Platform)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create a Platform client")
	}

	assistant := &Assistant{
		credentialsCfg: cfg,
		appCfg:         appCfg,
		msgProcessors:  make(map[string]connection.MessageProcessor),
		prefix:         prefix,
		featurePack:    pack,
		messenger:      messenger,
		userManager:    userManager,
		platformClient: platformClient,
	}

	return assistant, nil
}

func (a *Assistant) validateCredentials() error {
	if a.credentialsCfg == nil || a.credentialsCfg.AccessToken == "" || a.credentialsCfg.SigningSecret == "" {
		return errors.New(`"accessToken" and "signingSecret" must not be empty`)
	}

	return nil
}

// Init registers assistant handlers.
func (a *Assistant) Init(_ context.Context) error {
	log.Dbg("URL-path prefix: ", a.prefix)

	if err := a.validateCredentials(); err != nil {
		return errors.Wrap(err, "invalid credentials given")
	}

	if a.lenMessageProcessor() == 0 {
		return errors.New("no message processor set")
	}

	for path, handleFunc := range a.handlers() {
		http.Handle(fmt.Sprintf("%s/%s", a.prefix, path), handleFunc)
	}

	return nil
}

// AddChannel sets a message processor for a specific channel.
func (a *Assistant) AddChannel(channelID, project string, dbLabInstance *dblab.Instance) {
	messageProcessor := a.buildMessageProcessor(project, dbLabInstance)

	a.addProcessingService(channelID, messageProcessor)
}

func (a *Assistant) buildMessageProcessor(project string, dbLabInstance *dblab.Instance) *msgproc.ProcessingService {
	processingCfg := msgproc.ProcessingConfig{
		App:      a.appCfg.App,
		Platform: a.appCfg.Platform,
		Explain:  a.appCfg.Explain,
		DBLab:    dbLabInstance.Config(),
		EntOpts:  a.appCfg.Enterprise,
		Project:  project,
	}

	return msgproc.NewProcessingService(a.messenger, MessageValidator{}, dbLabInstance.Client(), a.userManager, a.platformClient,
		processingCfg, a.featurePack)
}

// addProcessingService adds a message processor for a specific channel.
func (a *Assistant) addProcessingService(channelID string, messageProcessor connection.MessageProcessor) {
	a.procMu.Lock()
	a.msgProcessors[channelID] = messageProcessor
	a.procMu.Unlock()
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
	log.Dbg("Check idle sessions", a.prefix)

	a.procMu.RLock()
	for _, proc := range a.msgProcessors {
		proc.CheckIdleSessions(ctx)
	}
	a.procMu.RUnlock()
}

func (a *Assistant) lenMessageProcessor() int {
	a.procMu.RLock()
	defer a.procMu.RUnlock()

	return len(a.msgProcessors)
}

func (a *Assistant) handlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"": a.handleEvent,
	}
}

func (a *Assistant) handleEvent(w http.ResponseWriter, r *http.Request) {
	log.Msg("Request received:", html.EscapeString(r.URL.Path))

	// TODO(anatoly): Respond time according to Slack API timeouts policy.
	// Slack sends retries in case of timeout responses.
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
		_, _ = w.Write([]byte(r.Challenge))

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
			msgProcessor.ProcessMessageEvent(context.TODO(), msg)

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
	message := unfurlLinks(event.Text)

	inputEvent := models.IncomingMessage{
		SubType:     event.SubType,
		Text:        message,
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

// unfurlLinks unfurls Slack links to the original content.
func unfurlLinks(text string) string {
	if strings.Contains(text, "<http:") {
		text = linkRegexp.ReplaceAllString(text, `$1`)
	}

	if strings.Contains(text, "<mailto:") {
		text = emailRegexp.ReplaceAllString(text, `$1`)
	}

	return text
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
