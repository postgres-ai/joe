/*
Joe Bot

2019 © Postgres.ai

Conversational UI bot for Postgres query optimization.
*/

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/jessevdk/go-flags"
	"github.com/sirupsen/logrus"
	"gitlab.com/postgres-ai/database-lab/pkg/client/dblabapi"
	"gitlab.com/postgres-ai/database-lab/pkg/log"
	"gopkg.in/yaml.v2"

	"gitlab.com/postgres-ai/joe/pkg/bot"
	"gitlab.com/postgres-ai/joe/pkg/chatapi"
	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
)

var opts struct {
	// Chat API.
	AccessToken       string `short:"t" long:"token" description:"\"Bot User OAuth Access Token\" which starts with \"xoxb-\"" env:"CHAT_TOKEN" required:"true"`
	VerificationToken string `short:"v" long:"verification-token" description:"callback URL verification token" env:"CHAT_VERIFICATION_TOKEN" required:"true"`

	// Database Lab.
	DBLabURL   string `long:"dblab-url" description:"Database Lab URL" env:"DBLAB_URL" default:"localhost"`
	DBLabToken string `long:"dblab-token" description:"Database Lab token" env:"DBLAB_TOKEN" default:"xxx"`

	DBName  string `short:"d" long:"dbname" description:"database name to connect to" env:"DBLAB_DBNAME" default:"db"`
	SSLMode string `long:"ssl-mode" description:"ssl mode provides different protection levels of a Database Lab connection." env:"DBLAB_SSL_MODE" default:"require"`

	// HTTP Server.
	ServerPort uint `short:"s" long:"http-port" description:"HTTP server port" env:"SERVER_PORT" default:"3001"`

	QuotaLimit    uint `long:"quota-limit" description:"limit request rates to up to 2x of this number" env:"QUOTA_LIMIT" default:"10"`
	QuotaInterval uint `long:"quota-interval" description:"an time interval (in seconds) to apply a quota-limit" env:"QUOTA_INTERVAL" default:"60"`

	// Platform.
	ApiUrl         string `long:"api-url" description:"Postgres.ai platform API base URL" env:"API_URL" default:"https://postgres.ai/api/general"`
	ApiToken       string `long:"api-token" description:"Postgres.ai platform API token" env:"API_TOKEN"`
	ApiProject     string `long:"api-project" description:"Postgres.ai platform project to assign user sessions" env:"API_PROJECT"`
	HistoryEnabled bool   `long:"history-enabled" description:"send command and queries history to Postgres.ai platform for collaboration and visualization" env:"HISTORY_ENABLED"`

	// Dev.
	DevGitCommitHash string `long:"git-commit-hash" env:"GIT_COMMIT_HASH" default:""`
	DevGitBranch     string `long:"git-branch" env:"GIT_BRANCH" default:""`
	DevGitModified   bool   `long:"git-modified" env:"GIT_MODIFIED"`

	Debug bool `long:"debug" description:"Enable a debug mode"`

	ShowHelp func() error `long:"help" description:"Show this help message"`
}

// TODO (akartasov): Set the app version during build.
const Version = "v0.4.0"

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

	version := formatBotVersion(opts.DevGitCommitHash, opts.DevGitBranch,
		opts.DevGitModified)

	log.Dbg("git: ", version)

	botCfg := config.Bot{
		Port:          opts.ServerPort,
		Explain:       explainConfig,
		QuotaLimit:    opts.QuotaLimit,
		QuotaInterval: opts.QuotaInterval,

		DBLab: config.DBLabInstance{
			URL:     opts.DBLabURL,
			Token:   opts.DBLabToken,
			DBName:  opts.DBName,
			SSLMode: opts.SSLMode,
		},

		ApiUrl:         opts.ApiUrl,
		ApiToken:       opts.ApiToken,
		ApiProject:     opts.ApiProject,
		HistoryEnabled: opts.HistoryEnabled,

		Version: version,
	}

	chat := chatapi.NewChat(opts.AccessToken, opts.VerificationToken)

	dbLabClient, err := dblabapi.NewClient(dblabapi.Options{
		Host:              botCfg.DBLab.URL,
		VerificationToken: botCfg.DBLab.Token,
	}, logrus.New())

	if err != nil {
		log.Fatal("Failed to create a Database Lab client", err)
	}

	joeBot := bot.NewBot(botCfg, chat, dbLabClient)
	joeBot.RunServer()
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
