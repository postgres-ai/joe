/*
2019 © Postgres.ai
*/

// Package transmission contains runners to translate user commands to retrieve meta information from storage.
package transmission

type Runner interface {
	Run(command string) (output string, err error)
}
