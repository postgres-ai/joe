/*
2019 © Postgres.ai
*/

// Package pgtransmission provides psql-commands transmission to retrieve meta information from a PostgreSQL clone.
package pgtransmission

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/pkg/log"
	"gitlab.com/postgres-ai/database-lab/pkg/services/provision/runners"

	"gitlab.com/postgres-ai/joe/pkg/dblab"
)

const (
	LogsEnabledDefault = true
	Hidden             = "HIDDEN"
)

type Transmitter struct {
	clone      dblab.Clone
	logEnabled bool
}

func NewPgTransmitter(clone dblab.Clone, logEnabled bool) *Transmitter {
	return &Transmitter{
		clone:      clone,
		logEnabled: logEnabled,
	}
}

func (tr Transmitter) Run(commandParam string) (string, error) {
	cmdStr, err := prepareCommandParam(commandParam)
	if err != nil {
		return "", errors.Wrapf(err, "failed to prepare command")
	}

	out, err := tr.runPsql(cmdStr)
	if err != nil {
		if runnerError, ok := err.(runners.RunnerError); ok {
			return "", fmt.Errorf("Psql error: %s", runnerError.Stderr)
		}

		return "", errors.Wrapf(err, "failed to execute command")
	}

	outFormatted := tr.format(out)

	return outFormatted, nil
}

func (tr Transmitter) runPsql(command string) ([]byte, error) {
	tempFile, err := ioutil.TempFile("", "psql-query-*")
	if err != nil {
		return nil, errors.WithStack(err)
	}

	defer func() {
		err := os.Remove(tempFile.Name())
		if err != nil {
			log.Err(err)
		}
	}()

	if _, err := tempFile.WriteString(command); err != nil {
		return nil, errors.WithStack(err)
	}

	if err := tempFile.Close(); err != nil {
		return nil, errors.WithStack(err)
	}

	cmdStr := fmt.Sprintf("%s psql %s -X -f %s",
		commandEnvString(tr.clone), commandConnString(tr.clone), tempFile.Name())

	return executeCommand(cmdStr)
}

// format formats output.
func (tr Transmitter) format(out []byte) string {
	outFormatted := bytes.Trim(out, " \n")

	logOut := []byte(Hidden)
	if tr.logEnabled {
		logOut = outFormatted
	}

	log.Dbg(fmt.Sprintf(`SQLRun: output "%s"`, string(logOut)))

	return string(outFormatted)
}

// Use for user defined commands to DB. Currently we only need
// to support limited number of PSQL meta information commands.
// That's why it's ok to restrict usage of some symbols.
func prepareCommandParam(command string) (string, error) {
	command = strings.Trim(command, " \n")
	if len(command) == 0 {
		return "", fmt.Errorf("Empty command")
	}

	// Psql file option (-f) allows to run any number of commands.
	// We need to take measures to restrict multiple commands support,
	// as we only check the first command.

	// User can run backslash commands on the same line with the first
	// backslash command (even without space separator),
	// e.g. `\d table1\d table2`.

	// Remove all backslashes except the one in the beggining.
	command = string(command[0]) + strings.ReplaceAll(command[1:], "\\", "")

	// Semicolumn creates possibility to run consequent command.
	command = strings.ReplaceAll(command, ";", "")

	// User can run any command (including DML queries) on other lines.
	// Restricting usage of multiline commands.
	command = strings.ReplaceAll(command, "\n", "")

	return command, nil
}

// commandEnvString returns a string of environment variables to use.
func commandEnvString(clone dblab.Clone) string {
	return fmt.Sprintf("PGPASSWORD=%s PGSSLMODE=%s", clone.Password, clone.SSLMode)
}

func commandConnString(clone dblab.Clone) string {
	return fmt.Sprintf("--host=%s --port=%s --user=%q --dbname=%q",
		clone.Host, clone.Port, clone.Username, clone.Name)
}

func executeCommand(cmdStr string) ([]byte, error) {
	log.Dbg(fmt.Sprintf(`SQLRun: "%s"`, cmdStr))

	var out bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.Command("/bin/bash", "-c", cmdStr)

	cmd.Stdout = &out
	cmd.Stderr = &stderr

	// Psql with the file option returns error reponse to stderr with
	// success exit code. In that case err will be nil, but we need
	// to treat the case as error and read proper output.
	err := cmd.Run()
	if err != nil || stderr.String() != "" {
		runnerError := runners.NewRunnerError(cmdStr, stderr.String(), err)

		log.Err(runnerError)
		return nil, runnerError
	}

	return out.Bytes(), nil
}
