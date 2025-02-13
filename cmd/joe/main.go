/*
Joe Bot

2019 © Postgres.ai

Conversational UI bot for Postgres query optimization.
*/

package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/log"

	"gitlab.com/postgres-ai/joe/features"
	"gitlab.com/postgres-ai/joe/pkg/bot"
	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/services/storage"
)

const (
	shutdownTimeout = 60 * time.Second
)

// ldflag variables.
var buildTime, version string

func main() {
	version := formatBotVersion()

	botCfg, err := loadAppConfig()
	if err != nil {
		log.Fatal("failed to load config: ", err)
	}

	log.SetDebug(botCfg.App.Debug)

	log.Dbg("version: ", version)

	botCfg.App.Version = version

	platformClient, err := platform.NewClient(botCfg.Platform)
	if err != nil {
		log.Fatal(errors.Wrap(err, "failed to create a Platform client"))
	}

	ctx, cancel := context.WithCancel(context.Background())
	shutdownCh := setShutdownListener()

	sessionsStorage := storage.NewJSONSessionData(path.Join(config.MetadataPath, config.SessionsFilename))
	if err := sessionsStorage.Load(); err != nil {
		log.Fatal("unable to load sessions data: ", err)
	}

	joeBot := bot.NewApp(botCfg, platformClient, features.NewPack(), sessionsStorage)

	go setSighupListener(ctx, joeBot)

	go func() {
		if err := joeBot.RunServer(ctx); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-shutdownCh
	log.Dbg("shutdown request received")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if err := joeBot.Shutdown(shutdownCtx); err != nil {
		log.Msg(err)
	}
}

func loadAppConfig() (*config.Config, error) {
	var (
		botCfg     config.Config
		configPath = path.Join(config.ConfigsPath, config.AppFilename)
	)

	if err := cleanenv.ReadConfig(configPath, &botCfg); err != nil {
		return nil, errors.Wrap(err, "failed to read a config file")
	}

	// Load and validate an enterprise options.
	enterpriseOptions, err := features.GetOptionProvider().GetEnterpriseOptions(configPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get enterprise options")
	}

	botCfg.Enterprise = enterpriseOptions

	return &botCfg, nil
}

func formatBotVersion() string {
	return version + "-" + buildTime
}

func setShutdownListener() chan os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	return c
}

// setSighupListener allows to dump active sessions.
func setSighupListener(ctx context.Context, app *bot.App) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c:
			if err := app.SaveSessions(); err != nil {
				log.Err("failed to save user session data: ", err)
			}
		}
	}
}
