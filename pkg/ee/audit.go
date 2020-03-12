/*
2019 © Postgres.ai
*/

// Package ee provides the Enterprise features.
package ee

// Audit represents audit log actions.
type Audit struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"realName"`
	Command  string `json:"command"`
	Query    string `json:"query"`
}
