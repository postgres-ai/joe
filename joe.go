/*
Joe Bot

2019 © Anatoly Stansler anatoly@postgres.ai
2019 © Postgres.ai

Conversational UI bot for Postgres query optimization.
Usage: 
TODO(anatoly): Fill up usage.
*/

package main

import (
	"fmt"
	"net/http"
	"bytes"
	"encoding/json"
	"html"
	"database/sql"
	"strings"

	"github.com/nlopes/slack"
	"github.com/nlopes/slack/slackevents"
	"github.com/jessevdk/go-flags"
	_ "github.com/lib/pq"
)

var opts struct {
	// Chat API
	AccessToken string `short:"t" long:"token" description:"\"Bot User OAuth Access Token\" which starts with \"xoxb-\"" env:"CHAT_TOKEN" required:"true"`
	VerificationToken string `short:"v" long:"verification-token" description:"callback URL verification token" env:"CHAT_VERIFICATION_TOKEN" required:"true"`

	// Database
	DBHost string `short:"h" long:"host" description:"database server host" env:"DB_HOST" default:"localhost"`
	DBPort uint `short:"p" long:"port" description:"database server port" env:"DB_PORT" default:"5432"`
	DBUser string `short:"U" long:"username" description:"database user name" env:"DB_USER" default:"postgres"`
	DBPassword string `short:"P" long:"password" description:"database password" env:"DB_PASSWORD" default:"postgres"`
	DBName string `short:"d" long:"dbname" description:"database name to connect to" env:"DB_NAME" default:"db"`

	ShowHelp func() error `long:"help" description:"Show this help message"`
}

func main() {
	var _, err = parseArgs()

	if err != nil {
		if flags.WroteHelp(err) {
			return
		}

		fmt.Println("[ERROR] Args parse error: ", err)
		return
	}

	fmt.Printf("[DEBUG] AccessToken: %s\n", opts.AccessToken)
	fmt.Printf("[DEBUG] VerificationToken: %s\n", opts.VerificationToken)
	fmt.Printf("[DEBUG] DBHost: %s\n", opts.DBHost)
	fmt.Printf("[DEBUG] DBPort: %d\n", opts.DBPort)
	fmt.Printf("[DEBUG] DBUser: %s\n", opts.DBUser)
	fmt.Printf("[DEBUG] DBPassword: %s\n", opts.DBPassword)
	fmt.Printf("[DEBUG] DBName: %s\n", opts.DBName)

	var connStr = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		opts.DBHost, opts.DBPort, opts.DBUser, opts.DBPassword, opts.DBName)
	var chatAPI = slack.New(opts.AccessToken)

	runHttpService(connStr, chatAPI)
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

// TODO(anatoly): Split main file to single responsibility modules.
func runHttpService(connStr string, chatAPI *slack.Client) {
	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("[INFO] Request received: ", html.EscapeString(r.URL.Path))

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
			fmt.Printf("%#v\n", innerEvent.Data)

			switch ev := innerEvent.Data.(type) {
			case *slackevents.AppMentionEvent:
				chatAPI.PostMessage(ev.Channel, slack.MsgOptionText("What's up?", false))
			case *slackevents.MessageEvent:
				// Skip messages sent by bots.
				if (ev.User == "" || ev.BotID != "") {
					return
				}

				var message = strings.TrimSpace(ev.Text)
				if (strings.HasPrefix(message, "query")) {
					var query = message[6:len(message)]

					var res, err = runQuery(connStr, "EXPLAIN " + query)
					if err != nil {
						fmt.Println("[ERROR] RunQuery: ", err)
						chatAPI.PostMessage(ev.Channel, slack.MsgOptionText("ERROR: " + err.Error(), false))
						return
					}
					chatAPI.PostMessage(ev.Channel, slack.MsgOptionText(res, false))

					res, err = runQuery(connStr, "EXPLAIN (ANALYZE, BUFFERS) " + query)
					if err != nil {
						fmt.Println("[ERROR] RunQuery: ", err)
						chatAPI.PostMessage(ev.Channel, slack.MsgOptionText("ERROR: " + err.Error(), false))
						return
					}
					chatAPI.PostMessage(ev.Channel, slack.MsgOptionText(res, false))
				}
			}
		}
	})
	fmt.Println("[INFO] Server listening")
	http.ListenAndServe(":3000", nil)
}

func runQuery(connStr string, query string) (string, error) {
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
