/*
2019 © Postgres.ai
*/

// Package slacksm provides the Slack SocketMode implementation of the communication interface.
package slacksm

import (
	"context"
	"fmt"
	"sync"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/pkg/errors"
	"github.com/slack-go/slack"

	"gitlab.com/postgres-ai/database-lab/v2/pkg/log"

	"gitlab.com/postgres-ai/joe/features"
	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	slack_assistent "gitlab.com/postgres-ai/joe/pkg/connection/slack"
	"gitlab.com/postgres-ai/joe/pkg/services/dblab"
	"gitlab.com/postgres-ai/joe/pkg/services/msgproc"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

// CommunicationType defines a workspace type.
const CommunicationType = "slacksm"

// Assistant provides a service for interaction with a communication channel.
type Assistant struct {
	credentialsCfg  *config.Credentials
	procMu          sync.RWMutex
	msgProcessors   map[string]connection.MessageProcessor
	appCfg          *config.Config
	featurePack     *features.Pack
	api             *slack.Client
	client          *socketmode.Client
	messenger       *slack_assistent.Messenger
	userManager     *usermanager.UserManager
	platformManager *platform.Client
}

// Config defines a slack configuration parameters.
type Config struct {
	AccessToken   string
	AppLevelToken string
}

// NewAssistant returns a new assistant service.
func NewAssistant(cfg *config.Credentials, appCfg *config.Config, pack *features.Pack, platformClient *platform.Client) *Assistant {
	slackCfg := &Config{
		AccessToken:   cfg.AccessToken,
		AppLevelToken: cfg.AppLevelToken,
	}

	api := slack.New(slackCfg.AccessToken,
		slack.OptionDebug(appCfg.App.Debug),
		slack.OptionAppLevelToken(cfg.AppLevelToken),
	)

	client := socketmode.New(
		api,
		socketmode.OptionDebug(appCfg.App.Debug),
	)

	messenger := slack_assistent.NewMessenger(api, &slack_assistent.MessengerConfig{
		AccessToken: slackCfg.AccessToken,
	})
	userInformer := slack_assistent.NewUserInformer(api)
	userManager := usermanager.NewUserManager(userInformer, appCfg.Enterprise.Quota)

	assistant := &Assistant{
		credentialsCfg:  cfg,
		appCfg:          appCfg,
		msgProcessors:   make(map[string]connection.MessageProcessor),
		featurePack:     pack,
		api:             api,
		client:          client,
		messenger:       messenger,
		userManager:     userManager,
		platformManager: platformClient,
	}

	return assistant
}

func (a *Assistant) validateCredentials() error {
	if a.credentialsCfg == nil || a.credentialsCfg.AccessToken == "" || a.credentialsCfg.AppLevelToken == "" {
		return errors.New(`"accessToken" and "appLevelToken" must not be empty`)
	}

	return nil
}

// Init initializes assistant handlers.
func (a *Assistant) Init() error {
	if err := a.validateCredentials(); err != nil {
		return errors.Wrap(err, "invalid credentials given")
	}

	if a.lenMessageProcessor() == 0 {
		return errors.New("no message processor set")
	}

	return nil
}

// Register registers the assistant service.
func (a *Assistant) Register(ctx context.Context, _ string) error {
	_, err := a.api.AuthTestContext(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to perform slack auth test")
	}

	// fail fast to ensure slack app is properly configured
	_, _, err = a.api.StartSocketModeContext(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to init slack socket mode")
	}

	go func() {
		err := a.client.RunContext(ctx)
		if err != nil {
			log.Errf("failed to run slack SocketMode assistant: ", err)
		}
	}()

	go a.handleSocketEvents(ctx, a.client.Events)

	return nil
}

// Deregister deregisters the assistant service.
func (a *Assistant) Deregister(_ context.Context) error {
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

	return msgproc.NewProcessingService(
		a.messenger,
		slack_assistent.MessageValidator{},
		dbLabInstance.Client(),
		a.userManager,
		a.platformManager,
		processingCfg,
		a.featurePack,
	)
}

func (a *Assistant) handleSocketEvents(ctx context.Context, incomingEvents chan socketmode.Event) {
	client := a.client

	var evt socketmode.Event

	for {
		select {
		case <-ctx.Done():
			return
		case evt = <-incomingEvents:
		}

		switch evt.Type {
		case socketmode.EventTypeConnecting:
			log.Dbg("Connecting to Slack with Socket Mode...")
		case socketmode.EventTypeConnectionError:
			log.Dbg("Connection failed. Retrying later...")
		case socketmode.EventTypeConnected:
			log.Dbg("Connected to Slack with Socket Mode.")
		case socketmode.EventTypeEventsAPI:
			eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
			if !ok {
				log.Dbg(fmt.Sprintf("Ignored %+v", evt))
				continue
			}

			log.Dbg(fmt.Sprintf("Event %s received: %+v", eventsAPIEvent.Type, eventsAPIEvent))
			client.Ack(*evt.Request)

			switch eventsAPIEvent.Type {
			case slackevents.CallbackEvent:
				switch ev := eventsAPIEvent.InnerEvent.Data.(type) {
				case *slackevents.MessageEvent:
					a.handleMessageEvent(ctx, ev)

				case *slackevents.AppMentionEvent:
					a.handleAppMentionEvent(ctx, ev)

				default:
					log.Dbg("Ignore message event: ", eventsAPIEvent.InnerEvent.Type)
				}

			default:
				client.Debugf("unsupported Events API event received")
			}

		case socketmode.EventTypeInteractive:
			_, ok := evt.Data.(slack.InteractionCallback)
			if !ok {
				log.Dbg(fmt.Sprintf("Ignored %+v", evt))
				continue
			}

			var payload interface{}

			client.Ack(*evt.Request, payload)
			log.Dbg("Ignore event type: ", evt.Type)

		default:
			log.Dbg("Ignore event type: ", evt.Type)
		}
	}
}

func (a *Assistant) handleMessageEvent(ctx context.Context, ev *slackevents.MessageEvent) {
	if ev.SubType != "" {
		// Handle only normal messages.
		return
	}

	if ev.BotID != "" {
		// Skip messages sent by bots.
		return
	}

	msgProcessor, err := a.getProcessingService(ev.Channel)
	if err != nil {
		log.Err("failed to get processing service", err)
		return
	}

	msg := slack_assistent.MessageEventToIncomingMessage(ev)
	go msgProcessor.ProcessMessageEvent(ctx, msg)
}

func (a *Assistant) handleAppMentionEvent(_ context.Context, ev *slackevents.AppMentionEvent) {
	log.Dbg(fmt.Sprintf("Desktop Notification: %v\n", ev))

	msgProcessor, err := a.getProcessingService(ev.Channel)
	if err != nil {
		log.Err("failed to get processing service", err)
		return
	}

	msg := slack_assistent.AppMentionEventToIncomingMessage(ev)
	msgProcessor.ProcessAppMentionEvent(msg)
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
	log.Dbg("Check Slack SocketMode idle sessions")

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
