/*
Joe Bot

2019 © Postgres.ai

Conversational UI bot for Postgres query optimization.
*/

package main

import (
	"context"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/features"
	"gitlab.com/postgres-ai/joe/pkg/bot"
	"gitlab.com/postgres-ai/joe/pkg/config"
)

// TODO (akartasov): Set the app version during build.
const Version = "v0.7.0"

var buildTime string

func main() {
	version := formatBotVersion()

	botCfg, err := loadConfig("config/config.yml")
	if err != nil {
		log.Fatal("failed to load config: %v", err)
	}

	log.DEBUG = botCfg.App.Debug

	log.Dbg("version: ", version)

	botCfg.App.Version = version

	joeBot := bot.NewApp(botCfg, features.NewPack())
	if err := joeBot.RunServer(context.Background()); err != nil {
		log.Err("HTTP server error:", err)
	}
}

func loadConfig(configPath string) (*config.Config, error) {
	var botCfg config.Config

	if err := cleanenv.ReadConfig(configPath, &botCfg); err != nil {
		return nil, errors.Wrap(err, "failed to read a config file")
	}

	// Load and validate an enterprise options.
	enterpriseOptions, err := features.GetOptionProvider().GetEnterpriseOptions(configPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get enterprise options")
	}

	botCfg.Enterprise = enterpriseOptions

	// Load and validate an explain configuration file.
	explainConfig, err := config.LoadExplainConfig()
	if err != nil {
		return nil, errors.Wrap(err, "failed to load an explain config")
	}

	botCfg.Explain = explainConfig

	return &botCfg, nil
}

func formatBotVersion() string {
	return Version + "-" + buildTime
}
