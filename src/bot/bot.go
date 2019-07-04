/*
2019 © Anatoly Stansler anatoly@postgres.ai
2019 © Postgres.ai
*/

package bot

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"../chat"
	"../log"
	"../pgexplain"
	"../provision"

	_ "github.com/lib/pq"
	"github.com/nlopes/slack"
	"github.com/nlopes/slack/slackevents"
)

// TODO(anatoly): Use chat package wrapper.

const SHOW_RAW_EXPLAIN = false

const COMMAND_QUERY = "query"
const COMMAND_EXEC = "exec"
const COMMAND_RESET = "reset"
const COMMAND_HARDRESET = "hardreset"
const COMMAND_HELP = "help"

var commands = []string{COMMAND_QUERY, COMMAND_EXEC, COMMAND_RESET, COMMAND_HARDRESET, COMMAND_HELP}

const MSG_HELP = "• `query` — analyze your query (SELECT, INSERT, DELETE, UPDATE or WITH) and generate recommendations\n" +
	"• `exec` — execute any query (for example, CREATE INDEX)\n" +
	"• `reset` — revert the database to the initial state (usually takes less than a minute, :warning: all changes will be lost)\n" +
	"• `hardreset` — re-provision the database instance (usually takes a couple of minutes, :warning: all changes will be lost)\n" +
	"• `help` — this message"

const MSG_QUERY_REQ = "Option query required for this command, e.g. `query select 1`"

const RCTN_RUNNING = "hourglass_flowing_sand"
const RCTN_OK = "white_check_mark"
const RCTN_ERROR = "x"

// TODO(anatoly): verifToken should be a part of Slack API wrapper.
// TODO(anatoly): Convert args to struct.
func RunHttpServer(connStr string, port uint, chatApi *slack.Client,
	explainConfig pgexplain.ExplainConfig, verifToken string, prov *provision.Provision) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Msg("Request received:", html.EscapeString(r.URL.Path))

		// TODO(anatoly): Respond time according to Slack API timeouts policy.
		// Slack sends retries in case of timedout responses.
		if r.Header.Get("X-Slack-Retry-Num") != "" {
			return
		}

		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		body := buf.String()
		log.Dbg("Request body:", body)

		eventsAPIEvent, err := slackevents.ParseEvent(json.RawMessage(body),
			slackevents.OptionVerifyToken(
				&slackevents.TokenComparator{VerificationToken: verifToken}))
		if err != nil {
			log.Err("Event parse error:", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		log.Dbg("EventsAPI event:", eventsAPIEvent)

		// Used to verified bot's API URL for Slack.
		if eventsAPIEvent.Type == slackevents.URLVerification {
			var r *slackevents.ChallengeResponse
			err := json.Unmarshal([]byte(body), &r)
			if err != nil {
				log.Err("Challenge parse error:", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text")
			w.Write([]byte(r.Challenge))
		}

		// General Slack events.
		if eventsAPIEvent.Type == slackevents.CallbackEvent {
			innerEvent := eventsAPIEvent.InnerEvent

			switch ev := innerEvent.Data.(type) {
			case *slackevents.AppMentionEvent:
				chatApi.PostMessage(ev.Channel, slack.MsgOptionText("What's up?", false))
			case *slackevents.MessageEvent:
				// Skip messages sent by bots.
				if ev.User == "" || ev.BotID != "" {
					return
				}

				var ch = ev.Channel
				var message = strings.TrimSpace(ev.Text)

				// Slack escapes some characters
				// https://api.slack.com/docs/message-formatting#how_to_escape_characters
				message = strings.ReplaceAll(message, "&amp;", "&")
				message = strings.ReplaceAll(message, "&lt;", "<")
				message = strings.ReplaceAll(message, "&gt;", ">")

				if len(message) == 0 {
					return
				}

				// Message: "command query(optional)".
				parts := strings.SplitN(message, " ", 2)
				command := parts[0]
				query := ""
				if len(parts) > 1 {
					query = parts[1]
				}

				if !contains(commands, command) {
					return
				}

				msg, err := chat.NewMessage(ch, chatApi)
				err = msg.Publish(fmt.Sprintf("```%s %s```", command, query))
				if err != nil {
					// TODO(anatoly): Retry.
					log.Err("Bot: Can't publish a message", err)
					return
				}

				runMsg(msg)

				switch command {
				case COMMAND_QUERY:
					if query == "" {
						failMsg(msg, MSG_QUERY_REQ)
						return
					}

					// Explain request and show.
					var res, err = runQuery(connStr, "EXPLAIN (FORMAT TEXT)"+query)
					if err != nil {
						failMsg(msg, err.Error())
						return
					}

					msg.Append(fmt.Sprintf("```%s```", res))

					// Explain analyze request and processing.
					res, err = runQuery(connStr,
						"EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) "+query)
					if err != nil {
						failMsg(msg, err.Error())
						return
					}

					if SHOW_RAW_EXPLAIN {
						msg.Append(res)
					}

					explain, err := pgexplain.NewExplain(res, explainConfig)
					if err != nil {
						log.Err("Explain parsing: ", err)
						failMsg(msg, err.Error())
						return
					}

					// Recommendations.
					tips, err := explain.GetTips()
					if err != nil {
						log.Err("Recommendations: ", err)
						failMsg(msg, err.Error())
						return
					}

					if len(tips) == 0 {
						msg.Append(":white_check_mark: Looks good")
					} else {
						recommends := "*Recommendations:*\n"
						for _, tip := range tips {
							recommends += fmt.Sprintf(
								":exclamation: %s – %s <example.com|Show details>\n", tip.Name,
								tip.Description)
						}
						msg.Append(recommends)
					}

					// Visualization.
					var buf = new(bytes.Buffer)
					explain.Visualize(buf)
					var vis = buf.String()

					msg.Append(fmt.Sprintf("*Explain Analyze Output:*\n```%s```", vis))
				case COMMAND_EXEC:
					if query == "" {
						failMsg(msg, MSG_QUERY_REQ)
						return
					}

					start := time.Now()
					var _, err = runQuery(connStr, query)
					elapsed := time.Since(start)
					if err != nil {
						log.Err("Exec:", err)
						failMsg(msg, err.Error())
						return
					}
					msg.Append(fmt.Sprintf("DDL executed. Execution Time: %s", elapsed))
				case COMMAND_RESET:
					msg.Append("Performing rollback of DB state...")
					err := prov.ResetSession()
					if err != nil {
						log.Err("Reset:", err)
						failMsg(msg, err.Error())
						return
					}
					msg.Append("Rollback performed")
				case COMMAND_HARDRESET:
					// Temprorary command for managing sessions.
					log.Msg("Reestablishing connection")
					msg.Append("Reestablishing connection to DB," +
						"it may take a couple of minutes...\n" +
						"If you want to rollback DB state use `reset` command.")

					prov.StopSession()

					// TODO(anatoly): Temp hack. Remove after provisioning fix.
					// "Can't attach pancake drive" bug.
					time.Sleep(2 * time.Second)
					prov.StopSession()

					res, sessionId, err := prov.StartSession()
					if err != nil {
						log.Err("Hardreset:", res, sessionId, err)
						failMsg(msg, err.Error())
						return
					}
					log.Msg("Connection reestablished", res, sessionId, err)
					msg.Append("Connection reestablished")
				case COMMAND_HELP:
					msg.Append(MSG_HELP)
				}

				okMsg(msg)
			}
		}
	})

	log.Msg("Server listening on", port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	log.Err("HTTP server error:", err)
}

// TODO(anatoly): Retries, error processing.
func runMsg(msg *chat.Message) {
	msg.ChangeReaction(RCTN_RUNNING)
}

func okMsg(msg *chat.Message) {
	msg.ChangeReaction(RCTN_OK)
}

func failMsg(msg *chat.Message, text string) {
	msg.Append(fmt.Sprintf("ERROR: %s", text))
	msg.ChangeReaction(RCTN_ERROR)
}

func runQuery(connStr string, query string) (string, error) {
	log.Dbg("DB query:", query)

	// TODO(anatoly): Retry mechanic.
	var result = ""

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Err("DB connection:", err)
		return "", err
	}

	rows, err := db.Query(query)
	if err != nil {
		log.Err("DB query:", err)
		return "", err
	}

	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			log.Err("DB query traversal:", err)
			return s, err
		}
		result += s + "\n"
	}
	if err := rows.Err(); err != nil {
		log.Err("DB query traversal:", err)
		return result, err
	}

	return result, nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if s == item {
			return true
		}
	}
	return false
}
