/*
Joe Bot

2019 © Anatoly Stansler anatoly@postgres.ai
2019 © Postgres.ai

Conversational UI bot for Postgres query optimization.
*/

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"os"
	"fmt"
	"html"
	"io/ioutil"
	"net/http"
	"strings"

	"./pgexplain"

	"github.com/jessevdk/go-flags"
	"github.com/nlopes/slack"
	"github.com/nlopes/slack/slackevents"
	"gopkg.in/yaml.v2"
	_ "github.com/lib/pq"
)

var opts struct {
	// Chat API
	AccessToken string `short:"t" long:"token" description:"\"Bot User OAuth Access Token\" which starts with \"xoxb-\"" env:"CHAT_TOKEN" required:"true"`
	VerificationToken string `short:"v" long:"verification-token" description:"callback URL verification token" env:"CHAT_VERIFICATION_TOKEN" required:"true"`

	// Database
	DbHost string `short:"h" long:"host" description:"database server host" env:"DB_HOST" default:"localhost"`
	DbPort uint `short:"p" long:"port" description:"database server port" env:"DB_PORT" default:"5432"`
	DbUser string `short:"U" long:"username" description:"database user name" env:"DB_USER" default:"postgres"`
	DbPassword string `short:"P" long:"password" description:"database password" env:"DB_PASSWORD" default:"postgres"`
	DbName string `short:"d" long:"dbname" description:"database name to connect to" env:"DB_NAME" default:"db"`

	// Provisioning
	AwsEnabled bool `short:"a" long:"aws" description:"provision ZFS instance on AWS" env:"AWS_ENABLED"`
	AwsKey string `short:"k" long:"aws-key" description:"AWS keypair name" env:"AWS_KEY"`
	AwsSshPath string `short:"l" long:"aws-ssh" description:"path to SSH key (.pem file)" env:"AWS_SSH_PATH"`
	AwsEc2Type string `long:"aws-ec2-type" description:"EC2 instance type" env:"AWS_EC2_TYPE" default:"i3.2xlarge"`
	AwsZone string `long:"aws-zone" description:"AWS zone" env:"AWS_ZONE" default:"a"`
	AwsDbEbsVolId string `long:"aws-db-ebs-vol-id" description:"EBS volume with DB data" env:"AWS_DB_EBS_VOL_ID"`

	// HTTP Server
	ServerPort uint `short:"s" long:"http-port" description:"HTTP server port" env:"SERVER_PORT" default:"3000"`

	ShowHelp func() error `long:"help" description:"Show this help message"`
}

const SHOW_RAW_EXPLAIN = false;

func main() {
	var _, err = parseArgs()

	if err != nil {
		if flags.WroteHelp(err) {
			return
		}

		fmt.Println("[ERROR] Args parse error: ", err)
		return
	}

	var connStr = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		opts.DbHost, opts.DbPort, opts.DbUser, opts.DbPassword, opts.DbName)
	var chatApi = slack.New(opts.AccessToken)

	explainConfig, err := loadExplainConfig()
	if err != nil {
		fmt.Println("[ERROR] Unable to load explain config: ", err)
		return
	}

	runHttpServer(connStr, opts.ServerPort, chatApi, explainConfig)
}

func parseArgs() ([]string, error) {
	var parser = flags.NewParser(&opts, flags.Default & ^flags.HelpFlag)

	// jessevdk/go-flags lib doesn't allow to use short flag -h because it's binded to usage help.
	// We need to hack it a bit to use -h for as a hostname option. See https://github.com/jessevdk/go-flags/issues/240
	opts.ShowHelp = func() error {
		var b bytes.Buffer

		parser.WriteHelp(&b)
		return &flags.Error{
			Type: flags.ErrHelp,
			Message: b.String(),
		}
	}

	return parser.Parse()
}

func loadExplainConfig() (pgexplain.ExplainConfig, error) {
	var config pgexplain.ExplainConfig

	b, err := ioutil.ReadFile(getConfigPath("explain.yaml"))
	if err != nil {
		return config, errors.New("Error loading explain config file")
	}
	
	err = yaml.Unmarshal(b, &config)
	if err != nil {
		return config, errors.New("Error parsing explain config")
	}

	fmt.Printf("[DEBUG] explainConfig:\n%v\n\n", config)

	return config, nil
}

func getConfigPath(name string) string {
	bindir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	dir, _ := filepath.Abs(filepath.Dir(bindir))
	path := dir + string(os.PathSeparator) + "config" + string(os.PathSeparator) + name
	return path
}

// TODO(anatoly): Split main file to single responsibility modules.
func runHttpServer(connStr string, port uint, chatApi *slack.Client, explainConfig pgexplain.ExplainConfig) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("[INFO] Request received: ", html.EscapeString(r.URL.Path))

		// TODO(anatoly): Respond time according to Slack API timeouts policy.
		// Slack sends retries in case of timedout responses.
		if r.Header.Get("X-Slack-Retry-Num") != "" {
			return
		}

		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		body := buf.String()
		fmt.Println("[DEBUG] Request body: ", body)

		eventsAPIEvent, err := slackevents.ParseEvent(json.RawMessage(body),
			slackevents.OptionVerifyToken(&slackevents.TokenComparator{VerificationToken: opts.VerificationToken}))
		if err != nil {
			fmt.Println("[ERROR] Event parse error: ", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		fmt.Printf("[INFO] EventsAPI event: %+v\n", eventsAPIEvent)

		// Used to verified bot's API URL for Slack.
		if eventsAPIEvent.Type == slackevents.URLVerification {
			var r *slackevents.ChallengeResponse
			err := json.Unmarshal([]byte(body), &r)
			if err != nil {
				fmt.Println("[ERROR] Challenge parse error: ", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text")
			w.Write([]byte(r.Challenge))
		}

		// General Slack events.
		if eventsAPIEvent.Type == slackevents.CallbackEvent {
			innerEvent := eventsAPIEvent.InnerEvent
			fmt.Printf("[DEBUG] %#v\n", innerEvent.Data)

			switch ev := innerEvent.Data.(type) {
			case *slackevents.AppMentionEvent:
				chatApi.PostMessage(ev.Channel, slack.MsgOptionText("What's up?", false))
			case *slackevents.MessageEvent:
				// Skip messages sent by bots.
				if (ev.User == "" || ev.BotID != "") {
					return
				}

				var message = strings.TrimSpace(ev.Text)
				if (strings.HasPrefix(message, "query")) {
					var query = message[6:len(message)]

					// Explain request and show.
					var res, err = runQuery(connStr, "EXPLAIN (FORMAT TEXT)" + query)
					if err != nil {
						fmt.Println("[ERROR] RunQuery: ", err)
						chatApi.PostMessage(ev.Channel, slack.MsgOptionText("ERROR: " + err.Error(), false))
						return
					}
					chatApi.PostMessage(ev.Channel, slack.MsgOptionText(":mag: `"+query+"`\n"+"```"+res+"```", false))

					// Explain analyze request and processing.
					res, err = runQuery(connStr, "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) " + query)
					if err != nil {
						fmt.Println("[ERROR] RunQuery: ", err)
						chatApi.PostMessage(ev.Channel, slack.MsgOptionText("ERROR: " + err.Error(), false))
						return
					}

					if (SHOW_RAW_EXPLAIN) {
						chatApi.PostMessage(ev.Channel, slack.MsgOptionText(res, false))
					}

					explain, err := pgexplain.NewExplain(res, explainConfig)
					if err != nil {
						fmt.Println("[ERROR] Explain parsing: ", err)
						chatApi.PostMessage(ev.Channel, slack.MsgOptionText("ERROR: " + err.Error(), false))
						return
					}

					// Recommendations.
					tips, err := explain.GetTips()
					if err != nil {
						fmt.Println("[ERROR] Recommendations: ", err)
						chatApi.PostMessage(ev.Channel, slack.MsgOptionText("ERROR: " + err.Error(), false))
						return
					}

					if len(tips) == 0 {
						chatApi.PostMessage(ev.Channel, slack.MsgOptionText(":white_check_mark: Looks good", false))
					} else {
						recommends := "Recommendations:\n"
						for _, tip := range tips {
							recommends += fmt.Sprintf(":red_circle: %s - %s\n", tip.Name, tip.Description)
						}
						chatApi.PostMessage(ev.Channel, slack.MsgOptionText(recommends, false))
					}

					// Visualization.
					var buf = new(bytes.Buffer)
					explain.Visualize(buf)
					var vis = buf.String()

					chatApi.PostMessage(ev.Channel, slack.MsgOptionText("```"+vis+"```", false))
				}
			}
		}
	})

	fmt.Printf("[INFO] Server listening on %d\n", port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	fmt.Printf("[ERROR] HTTP server: ", err)
}

func runQuery(connStr string, query string) (string, error) {
	// TODO(anatoly): Retry mechanic.
	var result = ""

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		fmt.Println("[ERROR] DB connection: ", err)
		return "", err
	}

	rows, err := db.Query(query)
	if err != nil {
		fmt.Println("[ERROR] DB query: ", err)
		return "", err
	}

	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			fmt.Println("[ERROR] DB query traversal: ", err)
			return s, err
		}
		result += s + "\n"
	}
	if err := rows.Err(); err != nil {
		fmt.Println("[ERROR] DB query traversal: ", err)
		return result, err
	}

	return result, nil
}
