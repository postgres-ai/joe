package models

// RunRequest represents run requests.
type RunRequest struct {
	DBLabServer string `json:"dblab_server"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	Password    string `json:"password"`
	SSLMode     string `json:"ssl_mode"`
	DBName      string `json:"db_name"`
	Command     string `json:"command"`
	Query       string `json:"query"`
	SessionID   string `json:"session_id"`
}

// RunResponse represents run response.
type RunResponse struct {
	SessionID   string `json:"session_id"`
	CommandID   string `json:"command_id"`
	CommandLink string `json:"command_link"`
}

// CommandResult represents the result of the running command.
type CommandResult struct {
	SessionID         string `json:"session_id"`
	Command           string `json:"command"`
	Query             string `json:"query"`
	PlanText          string `json:"plan_text"`
	PlanExecutionText string `json:"plan_execution_text"`
	Error             string `json:"error"`
	CreatedAt         string `json:"created_at"`
	Response          string `json:"response"`
	PlanJSON          string `json:"plan_json"`
	PlanExecutionJSON string `json:"plan_execution_json"`
	QueryLocks        string `json:"query_locks"`
}
