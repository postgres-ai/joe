/*
2019 © Postgres.ai
*/

package bot

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"gitlab.com/postgres-ai/database-lab/pkg/client/dblabapi"
	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/features"
	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	slackConnection "gitlab.com/postgres-ai/joe/pkg/connection/slack"
	"gitlab.com/postgres-ai/joe/pkg/services/dblab"
	"gitlab.com/postgres-ai/joe/pkg/util"
)

// InactiveCloneCheckInterval defines an interval for check of idleness sessions.
const InactiveCloneCheckInterval = time.Minute

// Workspace types
const (
	slackWorkspace = "slack"
)

// App defines a application struct.
type App struct {
	Config     config.Config
	spaceCfg   *config.Space
	enterprise *EEFeatures

	dblabMu        *sync.RWMutex
	dblabInstances map[string]*dblab.Instance
}

// EEFeatures defines enterprise feature helpers.
type EEFeatures struct {
	cmdBuilder features.CommandFactoryMethod
}

// NewEnterprise creates a new Enterprise struct.
func NewEnterprise(cmdBuilder features.CommandFactoryMethod) *EEFeatures {
	return &EEFeatures{cmdBuilder: cmdBuilder}
}

// Creates a new application.
func NewApp(cfg config.Config, spaceCfg *config.Space, enterprise *EEFeatures) *App {
	bot := App{
		Config:         cfg,
		spaceCfg:       spaceCfg,
		dblabMu:        &sync.RWMutex{},
		dblabInstances: make(map[string]*dblab.Instance, len(spaceCfg.DBLabInstances)),
		enterprise:     enterprise,
	}

	return &bot
}

// RunServer starts a server for message processing.
func (a *App) RunServer(ctx context.Context) error {
	if err := a.initDBLabInstances(); err != nil {
		return errors.Wrap(err, "failed to init Database Lab instances")
	}

	assistants, err := a.getAllAssistants()
	if err != nil {
		return errors.Wrap(err, "failed to get application assistants")
	}

	for _, assistantSvc := range assistants {
		if err := assistantSvc.Init(); err != nil {
			return errors.Wrap(err, "failed to init an assistant")
		}

		// Check idle sessions.
		_ = util.RunInterval(InactiveCloneCheckInterval, func() {
			log.Dbg("Check idle sessions")
			assistantSvc.CheckIdleSessions(ctx)
		})
	}

	port := a.Config.App.Port
	log.Msg(fmt.Sprintf("Server start listening on localhost:%d", port))

	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		return errors.Wrap(err, "failed to start a server")
	}

	return nil
}

func (a *App) initDBLabInstances() error {
	for name, dbLab := range a.spaceCfg.DBLabInstances {
		if err := a.validateDBLabInstance(dbLab); err != nil {
			return errors.Wrapf(err, "failed to init %q", name)
		}

		dbLabClient, err := dblabapi.NewClient(dblabapi.Options{
			Host:              dbLab.URL,
			VerificationToken: dbLab.Token,
		}, logrus.New())

		if err != nil {
			return errors.Wrap(err, "failed to create a Database Lab client")
		}

		a.dblabMu.Lock()
		a.dblabInstances[name] = dblab.NewDBLabInstance(dbLabClient, dbLab)
		a.dblabMu.Unlock()
	}

	return nil
}

func (a *App) validateDBLabInstance(instance config.DBLabInstance) error {
	if instance.URL == "" || instance.Token == "" || instance.DBName == "" {
		return errors.New("invalid DBLab Instance config given")
	}

	return nil
}

func (a *App) getAllAssistants() ([]connection.Assistant, error) {
	assistants := []connection.Assistant{}

	for workspaceType, workspaceList := range a.spaceCfg.Connections {
		for _, workspace := range workspaceList {
			assist, err := a.getAssistant(workspaceType, workspace)
			if err != nil {
				return nil, errors.Wrap(err, "failed to register workspace assistants")
			}

			assist.SetHandlerPrefix(fmt.Sprintf("/%s", workspaceType))

			if err := a.setupDBLabInstances(assist, workspace); err != nil {
				return nil, errors.Wrap(err, "failed to register workspace assistants")
			}

			assistants = append(assistants, assist)
		}
	}

	return assistants, nil
}

func (a *App) getAssistant(workspaceType string, workspaceCfg config.Workspace) (connection.Assistant, error) {
	switch workspaceType {
	case slackWorkspace:
		return slackConnection.NewAssistant(&workspaceCfg.Credentials, &a.Config, a.enterprise.cmdBuilder)

	default:
		return nil, errors.New("unknown workspace type given")
	}
}

func (a *App) setupDBLabInstances(assistant connection.Assistant, workspace config.Workspace) error {
	for _, channel := range workspace.Channels {
		a.dblabMu.RLock()

		dbLabInstance, ok := a.dblabInstances[channel.DBLabID]
		if !ok {
			a.dblabMu.RUnlock()
			return errors.Errorf("failed to find a configuration of the Database Lab client: %q", channel.DBLabID)
		}

		a.dblabMu.RUnlock()

		assistant.AddDBLabInstanceForChannel(channel.ChannelID, dbLabInstance)
	}

	return nil
}
