/*
Joe Bot

2019 © Anatoly Stansler anatoly@postgres.ai
2019 © Dmitry Udalov dmius@postgres.ai
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

	"./bot"
	"./ec2ctrl"
	"./log"
	"./pgexplain"
	"./provision"

	"github.com/jessevdk/go-flags"
	"github.com/nlopes/slack"
	"gopkg.in/yaml.v2"
)

var opts struct {
	// Chat API
	AccessToken       string `short:"t" long:"token" description:"\"Bot User OAuth Access Token\" which starts with \"xoxb-\"" env:"CHAT_TOKEN" required:"true"`
	VerificationToken string `short:"v" long:"verification-token" description:"callback URL verification token" env:"CHAT_VERIFICATION_TOKEN" required:"true"`

	// Database
	DbHost     string `short:"h" long:"host" description:"database server host" env:"DB_HOST" default:"localhost"`
	DbPort     uint   `short:"p" long:"port" description:"database server port" env:"DB_PORT" default:"5432"`
	DbUser     string `short:"U" long:"username" description:"database user name" env:"DB_USER" default:"postgres"`
	DbPassword string `short:"P" long:"password" description:"database password" env:"DB_PASSWORD" default:"postgres"`
	DbName     string `short:"d" long:"dbname" description:"database name to connect to" env:"DB_NAME" default:"db"`

	// HTTP Server
	ServerPort uint `short:"s" long:"http-port" description:"HTTP server port" env:"SERVER_PORT" default:"3000"`

	ShowHelp func() error `long:"help" description:"Show this help message"`
}

type ProvisionConfig struct {
	Local              bool                         `yaml:"local"`
	AwsConfiguration   ec2ctrl.Ec2Configuration     `yaml:"awsConfiguration"`
	LocalConfiguration provision.LocalConfiguration `yaml:"localConfiguration"`
	EbsVolumeId        string                       `yaml:"ebsVolumeId"`
	Debug              bool                         `yaml:"debug"`
	PgVersion          string                       `yaml:"pgVersion"`
	InitialSnapshot    string                       `yaml:"initialSnapshot"`
}

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

	// Load and validate configuration files.
	explainConfig, err := loadExplainConfig()
	if err != nil {
		log.Err("Unable to load explain config", err)
		return
	}
	provisionConfig, err := loadProvisionConfig()
	if err != nil {
		log.Err("Unable to load provision config", err)
		return
	}
	log.DEBUG = provisionConfig.Debug
	provConf := provision.ProvisionConfiguration{
		AwsConfiguration:   provisionConfig.AwsConfiguration,
		LocalConfiguration: provisionConfig.LocalConfiguration,
		Local:              provisionConfig.Local,
		Debug:              provisionConfig.Debug,
		EbsVolumeId:        provisionConfig.EbsVolumeId,
		InitialSnapshot:    provisionConfig.InitialSnapshot,
		PgVersion:          provisionConfig.PgVersion,
		DbUsername:         opts.DbUser,
		DbPassword:         opts.DbPassword,
		SshTunnelPort:      opts.DbPort,
	}
	if !provision.IsValidConfig(provConf) {
		log.Err("Wrong configuration format.")
		os.Exit(1)
	}

	// Start AWS instance and set up SSH tunnel for PG connection.
	prov := provision.NewProvision(provConf)
	prov.StopSession()
	res, sessionId, err := prov.StartSession()
	if err != nil {
		log.Fatal("Start session error", res, sessionId, err)
	} else {
		log.Msg("Session started", res, sessionId, err)
	}

	var connStr = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		opts.DbHost, opts.DbPort, opts.DbUser, opts.DbPassword, opts.DbName)
	var chatApi = slack.New(opts.AccessToken)
	bot.RunHttpServer(connStr, opts.ServerPort, chatApi, explainConfig, opts.VerificationToken, prov)

	prov.StopSession()
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

func loadProvisionConfig() (ProvisionConfig, error) {
	var config = ProvisionConfig{
		AwsConfiguration: ec2ctrl.Ec2Configuration{
			AwsInstanceType: "r4.large",
			AwsRegion:       "us-east-1",
			AwsZone:         "a",
		},
		Local:           false,
		Debug:           true,
		PgVersion:       "9.6",
		InitialSnapshot: "db_state_1",
	}

	err := loadConfig(&config, "provisioning.yaml")
	if err != nil {
		return config, err
	}

	return config, nil
}

func loadConfig(config interface{}, name string) error {
	b, err := ioutil.ReadFile(getConfigPath(name))
	if err != nil {
		return fmt.Errorf("Error loading %s config file", name)
	}

	err = yaml.Unmarshal(b, config)
	if err != nil {
		return fmt.Errorf("Error parsing %s config", name)
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
