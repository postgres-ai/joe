/*
2019 © Postgres.ai
*/

// Package structs provides domain entities.
package models

import "fmt"

// Clone contains connection info of a clone.
type Clone struct {
	Name     string
	Host     string
	Port     string
	Username string
	Password string
	SSLMode  string
}

func (clone Clone) ConnectionString() string {
	return fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s sslmode=%s",
		clone.Host, clone.Port, clone.Username, clone.Name, clone.Password, clone.SSLMode)
}
