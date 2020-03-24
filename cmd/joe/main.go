/*
Joe Bot

2019 © Postgres.ai

Conversational UI bot for Postgres query optimization.
*/

package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/jessevdk/go-flags"
	"gopkg.in/yaml.v2"

	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/bot"
	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
)

var opts struct {
	// HTTP Server.
	ServerPort uint `short:"s" long:"http-port" description:"HTTP server port" env:"SERVER_PORT" default:"3001"`

	MinNotifyDuration uint `long:"min-notify-duration" description:"a time interval (in minutes) to notify a user about the finish of a long query" env:"MIN_NOTIFY_DURATION" default:"1"`

	// Platform.
	PlatformURL     string `long:"api-url" description:"Postgres.ai platform API base URL" env:"API_URL" default:"https://postgres.ai/api/general"` // nolint:lll
	PlatformToken   string `long:"api-token" description:"Postgres.ai platform API token" env:"API_TOKEN"`
	PlatformProject string `long:"api-project" description:"Postgres.ai platform project to assign user sessions" env:"API_PROJECT"`
	HistoryEnabled  bool   `long:"history-enabled" description:"send command and queries history to Postgres.ai platform for collaboration and visualization" env:"HISTORY_ENABLED"` // nolint:lll

	// Dev.
	DevGitCommitHash string `long:"git-commit-hash" env:"GIT_COMMIT_HASH" default:""`
	DevGitBranch     string `long:"git-branch" env:"GIT_BRANCH" default:""`
	DevGitModified   bool   `long:"git-modified" env:"GIT_MODIFIED"`

	Debug bool `long:"debug" description:"Enable a debug mode"`

	ShowHelp func() error `long:"help" description:"Show this help message"`

	// Enterprise features (changing these options you confirm that you have active subscription to Postgres.ai Platform Enterprise Edition https://postgres.ai).
	QuotaLimit    uint `long:"quota-limit" description:"limit request rates to up to 2x of this number" env:"EE_QUOTA_LIMIT" default:"10"`
	QuotaInterval uint `long:"quota-interval" description:"a time interval (in seconds) to apply a quota-limit" env:"EE_QUOTA_INTERVAL" default:"60"`
	AuditEnabled  bool `long:"audit-enabled" description:"enable logging of received commands" env:"EE_AUDIT_ENABLED"`
}

// TODO (akartasov): Set the app version during build.
const Version = "v0.6.1-rc1"

// TODO(anatoly): Refactor configs and envs.

func main() {
	// Load CLI options.
	var _, err = parseArgs()

	if err != nil {
		if flags.WroteHelp(err) {
			return
		}

		log.Err("Args parse error", err)
		return
	}

	log.DEBUG = opts.Debug

	// Load and validate configuration files.
	explainConfig, err := loadExplainConfig()
	if err != nil {
		log.Err("Unable to load explain config", err)
		return
	}

	version := formatBotVersion(opts.DevGitCommitHash, opts.DevGitBranch, opts.DevGitModified)

	log.Dbg("git: ", version)

	spaceCfg, err := config.Load("config/config.yml")
	if err != nil {
		log.Fatal(err)
	}

	botCfg := config.Config{
		App: config.App{
			Version:                  version,
			Port:                     opts.ServerPort,
			AuditEnabled:             opts.AuditEnabled,
			MinNotifyDurationMinutes: opts.MinNotifyDuration,
		},
		Explain: explainConfig,
		Quota: config.Quota{
			Limit:    opts.QuotaLimit,
			Interval: opts.QuotaInterval,
		},


		Platform: config.Platform{
			URL:            opts.PlatformURL,
			Token:          opts.PlatformToken,
			Project:        opts.PlatformProject,
			HistoryEnabled: opts.HistoryEnabled,
		},
	}

	joeBot := bot.NewApp(botCfg, spaceCfg)
	if err := joeBot.RunServer(context.Background()); err != nil {
		log.Err("HTTP server error:", err)
	}
}

func parseArgs() ([]string, error) {
	var parser = flags.NewParser(&opts, flags.Default & ^flags.HelpFlag)

	// jessevdk/go-flags lib doesn't allow to use short flag -h because it's binded to usage help.
	// We need to hack it a bit to use -h for as a hostname option. See https://github.com/jessevdk/go-flags/issues/240
	opts.ShowHelp = func() error {
		var b bytes.Buffer

		parser.WriteHelp(&b)
		return &flags.Error{
			Type:    flags.ErrHelp,
			Message: b.String(),
		}
	}

	return parser.Parse()
}

func loadExplainConfig() (pgexplain.ExplainConfig, error) {
	var config pgexplain.ExplainConfig

	err := loadConfig(&config, "explain.yaml")
	if err != nil {
		return config, err
	}

	return config, nil
}

func loadConfig(config interface{}, name string) error {
	b, err := ioutil.ReadFile(getConfigPath(name))
	if err != nil {
		return fmt.Errorf("Error loading %s config file: %v", name, err)
	}

	err = yaml.Unmarshal(b, config)
	if err != nil {
		return fmt.Errorf("Error parsing %s config: %v", name, err)
	}

	log.Dbg("Config loaded", name, config)
	return nil
}

func getConfigPath(name string) string {
	bindir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	dir, _ := filepath.Abs(filepath.Dir(bindir))
	path := dir + string(os.PathSeparator) + "config" + string(os.PathSeparator) + name
	return path
}

func formatBotVersion(commit string, branch string, modified bool) string {
	if len(commit) < 7 {
		return Version
	}

	modifiedStr := ""
	if modified {
		modifiedStr = " (modified)"
	}

	commitShort := commit[:7]

	return fmt.Sprintf("%s@%s%s", commitShort, branch, modifiedStr)
}
