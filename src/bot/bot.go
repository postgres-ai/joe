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

	"../log"
	"../pgexplain"
	"../provision"

	_ "github.com/lib/pq"
	"github.com/nlopes/slack"
	"github.com/nlopes/slack/slackevents"
)

const SHOW_RAW_EXPLAIN = false

// TODO(anatoly): verifToken should be a part of Slack API wrapper.
// TODO(anatoly): Convert args to struct.
func RunHttpServer(connStr string, port uint, chatApi *slack.Client, explainConfig pgexplain.ExplainConfig, verifToken string, prov *provision.Provision) {
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
			slackevents.OptionVerifyToken(&slackevents.TokenComparator{VerificationToken: verifToken}))
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

				if strings.HasPrefix(message, "query") {
					var query = message[6:len(message)]

					// Explain request and show.
					var res, err = runQuery(connStr, "EXPLAIN (FORMAT TEXT)"+query)
					if err != nil {
						log.Err("Query: ", err)
						postMsg(chatApi, ch, "ERROR: "+err.Error())
						return
					}

					postMsg(chatApi, ch, fmt.Sprintf("```%s```\n"+"```%s```", query, res))

					// Explain analyze request and processing.
					res, err = runQuery(connStr, "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) "+query)
					if err != nil {
						log.Err("Query: ", err)
						postMsg(chatApi, ch, "ERROR: "+err.Error())
						return
					}

					if SHOW_RAW_EXPLAIN {
						postMsg(chatApi, ch, res)
					}

					explain, err := pgexplain.NewExplain(res, explainConfig)
					if err != nil {
						log.Err("Explain parsing: ", err)
						postMsg(chatApi, ch, "ERROR: "+err.Error())
						return
					}

					// Recommendations.
					tips, err := explain.GetTips()
					if err != nil {
						log.Err("Recommendations: ", err)
						postMsg(chatApi, ch, "ERROR: "+err.Error())
						return
					}

					if len(tips) == 0 {
						postMsg(chatApi, ch, ":white_check_mark: Looks good")
					} else {
						recommends := "Recommendations:\n"
						for _, tip := range tips {
							recommends += fmt.Sprintf(":red_circle: %s - %s\n", tip.Name, tip.Description)
						}
						postMsg(chatApi, ch, recommends)
					}

					// Visualization.
					var buf = new(bytes.Buffer)
					explain.Visualize(buf)
					var vis = buf.String()

					postMsg(chatApi, ch, fmt.Sprintf("```%s```", vis))
				} else if strings.HasPrefix(message, "reset") {
					postMsg(chatApi, ch, "Performing rollback of DB state...")
					err := prov.ResetSession()
					if err != nil {
						log.Err("Reset:", err)
						postMsg(chatApi, ch, "ERROR: "+err.Error())
						return
					}
					postMsg(chatApi, ch, "Rollback performed")
				} else if strings.HasPrefix(message, "hardreset") {
					// Temprorary command for managing sessions.
					log.Msg("Reestablishing connection")
					postMsg(chatApi, ch, "Reestablishing connection to DB, it may take a couple of minutes...\n"+
						"If you want to rollback DB state use `reset` command.")
					prov.StopSession()
					res, sessionId, err := prov.StartSession()
					if err != nil {
						log.Err("Hardreset:", res, sessionId, err)
						postMsg(chatApi, ch, "ERROR: "+err.Error())
						return
					}
					log.Msg("Connection reestablished", res, sessionId, err)
					postMsg(chatApi, ch, "Connection reestablished")
				} else if strings.HasPrefix(message, "exec") {
					//TODO(anatoly): Restrict insecure operations and data access.
					var query = message[5:len(message)]
					postMsg(chatApi, ch, fmt.Sprintf(":rocket: `%s`", query))

					start := time.Now()
					var _, err = runQuery(connStr, query)
					elapsed := time.Since(start)
					if err != nil {
						log.Err("Exec:", err)
						postMsg(chatApi, ch, "ERROR: "+err.Error())
						return
					}
					postMsg(chatApi, ch, fmt.Sprintf("DDL executed. Execution Time: %s", elapsed))
				}
			}
		}
	})

	log.Msg("Server listening on", port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	log.Err("HTTP server error:", err)
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

func postMsg(chatApi *slack.Client, ch string, msg string) {
	chatApi.PostMessage(ch, slack.MsgOptionText(msg, false))
}
