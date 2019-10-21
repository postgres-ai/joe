/*
2019 © Postgres.ai
*/

package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"../log"
)

type ApiCommand struct {
	SessionId string `json:"session_id"`

	Command  string `json:"command"`
	Query    string `json:"query"`
	Response string `json:"response"`

	// Explain.
	PlanText        string `json:"plan_text"`
	PlanJson        string `json:"plan_json"`
	PlanExecText    string `json:"plan_execution_text"`
	PlanExecJson    string `json:"plan_execution_json"`
	Recommendations string `json:"recommendations"`
	Stats           string `json:"stats"`

	Error string `json:"error"`

	SlackTs string `json:"slack_ts"`

	AccessToken string `json:"access_token"`
}

type ApiSession struct {
	ProjectName   string `json:"project_name"`
	AccessToken   string `json:"access_token"`
	SlackUid      string `json:"slack_uid"`
	SlackUsername string `json:"slack_username"`
	SlackChannel  string `json:"slack_channel"`
}

type ApiResp struct {
	Hint    string `json:"hint"`
	Details string `json:"details"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ApiCreateSessionResp struct {
	ApiResp
	SessionId uint `json:"session_id"`
}

type ApiPostCommandResp struct {
	ApiResp
	CommandId uint `json:"command_id"`
}

func (b *Bot) ApiCreateSession(uid string, username string, channel string) (string, error) {
	log.Dbg("API: Create session")

	reqData, err := json.Marshal(&ApiSession{
		ProjectName:   b.Config.ApiProject,
		AccessToken:   b.Config.ApiToken,
		SlackUid:      uid,
		SlackUsername: username,
		SlackChannel:  channel,
	})
	if err != nil {
		return "", err
	}

	resp, err := http.Post(b.Config.ApiUrl+"/rpc/joe_session_create",
		"application/json", bytes.NewBuffer(reqData))
	if err != nil {
		return "", err
	}

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	respData := ApiCreateSessionResp{}
	err = json.Unmarshal(bodyBytes, &respData)
	if err != nil {
		return "", err
	}

	if len(respData.Code) > 0 || len(respData.Message) > 0 {
		return "", fmt.Errorf("Error: %v", respData)
	}

	log.Dbg("API: Create session success", respData.SessionId)
	return fmt.Sprintf("%d", respData.SessionId), nil
}

func (b *Bot) ApiPostCommand(command *ApiCommand) (string, error) {
	log.Dbg("API: Post command")

	command.AccessToken = b.Config.ApiToken

	reqData, err := json.Marshal(command)
	if err != nil {
		return "", err
	}

	log.Dbg(string(reqData))

	resp, err := http.Post(b.Config.ApiUrl+"/rpc/joe_session_command_post",
		"application/json", bytes.NewBuffer(reqData))
	if err != nil {
		return "", err
	}

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	respData := ApiPostCommandResp{}
	err = json.Unmarshal(bodyBytes, &respData)
	if err != nil {
		return "", err
	}

	if len(respData.Code) > 0 || len(respData.Message) > 0 {
		return "", fmt.Errorf("Error: %v", respData)
	}

	log.Dbg("API: Post command success", respData.CommandId)
	return fmt.Sprintf("%d", respData.CommandId), nil
}
