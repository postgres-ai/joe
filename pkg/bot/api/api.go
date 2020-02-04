/*
2019 © Postgres.ai
*/

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"gitlab.com/postgres-ai/database-lab/pkg/log"
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
	ApiURL      string `json:"-"`
}

func (command *ApiCommand) Post() (string, error) {
	log.Dbg("API: Post command")

	reqData, err := json.Marshal(command)
	if err != nil {
		return "", err
	}

	log.Dbg(string(reqData))

	resp, err := http.Post(command.ApiURL+"/rpc/joe_session_command_post",
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

func (apiCmd *ApiCommand) Fail(text string) {
	apiCmd.Error = text
	_, err := apiCmd.Post()
	if err != nil {
		log.Err("failApiCmd:", err)
	}
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
