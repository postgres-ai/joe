/*
2019 © Postgres.ai
*/

// Package models provides domain entities.
package models

// Audit represents audit log actions.
type Audit struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"realName"`
	Command  string `json:"command"`
	Query    string `json:"query"`
}
