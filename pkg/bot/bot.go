/*
2019 © Postgres.ai
*/

package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
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
	"gitlab.com/postgres-ai/joe/pkg/connection/slack"
	"gitlab.com/postgres-ai/joe/pkg/connection/webui"
	"gitlab.com/postgres-ai/joe/pkg/services/dblab"
	"gitlab.com/postgres-ai/joe/pkg/util"
)

// InactiveCloneCheckInterval defines an interval for check of idleness sessions.
const InactiveCloneCheckInterval = time.Minute

// App defines a application struct.
type App struct {
	Config      *config.Config
	featurePack *features.Pack

	dblabMu        *sync.RWMutex
	dblabInstances map[string]*dblab.Instance
}

// HealthResponse represents a response for heath-check requests.
type HealthResponse struct {
	Version            string   `json:"version"`
	Edition            string   `json:"edition"`
	CommunicationTypes []string `json:"communication_types"`
}

// Creates a new application.
func NewApp(cfg *config.Config, enterprise *features.Pack) *App {
	bot := App{
		Config:         cfg,
		dblabMu:        &sync.RWMutex{},
		dblabInstances: make(map[string]*dblab.Instance, len(cfg.ChannelMapping.DBLabInstances)),
		featurePack:    enterprise,
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

		svc := assistantSvc
		// Check idle sessions.
		_ = util.RunInterval(InactiveCloneCheckInterval, func() {
			svc.CheckIdleSessions(ctx)
		})
	}

	http.HandleFunc("/", a.healthCheck)

	log.Msg(fmt.Sprintf("Server start listening on %s:%d", a.Config.App.Host, a.Config.App.Port))

	if err := http.ListenAndServe(fmt.Sprintf("%s:%d", a.Config.App.Host, a.Config.App.Port), nil); err != nil {
		return errors.Wrap(err, "failed to start a server")
	}

	return nil
}

func (a *App) initDBLabInstances() error {
	if len(a.Config.ChannelMapping.DBLabInstances) > int(a.Config.Enterprise.DBLab.InstanceLimit) {
		return errors.Errorf("available limit exceeded, the maximum amount is %d. "+
			"Please correct the `dblabs` section in the configuration file or upgrade your plan to Enterprise Edition",
			a.Config.Enterprise.DBLab.InstanceLimit)
	}

	for name, dbLab := range a.Config.ChannelMapping.DBLabInstances {
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
		a.dblabInstances[name] = dblab.NewDBLabInstance(dbLabClient)
		a.dblabMu.Unlock()
	}

	return nil
}

func (a *App) validateDBLabInstance(instance config.DBLabInstance) error {
	if instance.URL == "" || instance.Token == "" {
		return errors.New("invalid DBLab Instance config given")
	}

	return nil
}

func (a *App) getAllAssistants() ([]connection.Assistant, error) {
	assistants := []connection.Assistant{}

	for workspaceType, workspaceList := range a.Config.ChannelMapping.CommunicationTypes {
		for _, workspace := range workspaceList {
			assist, err := a.getAssistant(workspaceType, workspace)
			if err != nil {
				return nil, errors.Wrap(err, "failed to register workspace assistants")
			}

			if err := a.setupDBLabInstances(assist, workspace); err != nil {
				return nil, errors.Wrap(err, "failed to register workspace assistants")
			}

			assistants = append(assistants, assist)
		}
	}

	return assistants, nil
}

func (a *App) getAssistant(communicationTypeType string, workspaceCfg config.Workspace) (connection.Assistant, error) {
	handlerPrefix := fmt.Sprintf("/%s", communicationTypeType)

	switch communicationTypeType {
	case slack.CommunicationType:
		return slack.NewAssistant(&workspaceCfg.Credentials, a.Config, handlerPrefix, a.featurePack), nil

	case webui.CommunicationType:
		return webui.NewAssistant(&workspaceCfg.Credentials, a.Config, handlerPrefix, a.featurePack), nil

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
		dbLabInstance.SetCfg(channel.DBLabParams)

		if err := assistant.AddDBLabInstanceForChannel(channel.ChannelID, dbLabInstance); err != nil {
			return errors.Wrap(err, "failed to add a DBLab instance")
		}
	}

	return nil
}

// healthCheck handles health-check requests.
func (a *App) healthCheck(w http.ResponseWriter, r *http.Request) {
	log.Msg("Health check received:", html.EscapeString(r.URL.Path))

	communicationTypes := make([]string, 0, len(a.Config.ChannelMapping.CommunicationTypes))

	for typeName := range a.Config.ChannelMapping.CommunicationTypes {
		communicationTypes = append(communicationTypes, typeName)
	}

	healthResponse := HealthResponse{
		Version:            a.Config.App.Version,
		Edition:            a.featurePack.Entertainer().GetEdition(),
		CommunicationTypes: communicationTypes,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if err := json.NewEncoder(w).Encode(healthResponse); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Err(err)

		return
	}
}
