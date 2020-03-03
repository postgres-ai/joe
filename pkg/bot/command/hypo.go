/*
2019 © Postgres.ai
*/

package command

import (
	"database/sql"
	"strings"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/pkg/bot/api"
	"gitlab.com/postgres-ai/joe/pkg/bot/querier"
	"gitlab.com/postgres-ai/joe/pkg/chatapi"
)

// Hypo sub-commands
const (
	hypoCreate = "create"
	hypoDesc   = "desc"
	hypoDrop   = "drop"
	hypoReset  = "reset"
)

// HypoPGCaption contains caption for rendered tables.
const HypoPGCaption = "*HypoPG response:*\n"

// hypoPGExceptionMessage  defines an error message on failure of extension initialize.
const hypoPGExceptionMessage = `:warning: Cannot init the HypoPG extension.
Make sure that the extension has been installed in your Postgres image for Database Lab: https://postgres.ai/docs/database-lab/supported_databases.
For a quick start, you can use prepared images: https://hub.docker.com/repository/docker/postgresai/extended-postgres created by *Postgres.ai*, or prepare your own.`

// HypoCmd defines a hypo command.
type HypoCmd struct {
	apiCommand *api.ApiCommand
	message    *chatapi.Message
	db         *sql.DB
}

// NewHypo creates a new Hypo command.
func NewHypo(apiCmd *api.ApiCommand, msg *chatapi.Message, db *sql.DB) *HypoCmd {
	return &HypoCmd{
		apiCommand: apiCmd,
		message:    msg,
		db:         db,
	}
}

// Execute runs the hypo command.
func (h *HypoCmd) Execute() error {
	hypoSub, commandTail := h.parseQuery()

	if err := h.initExtension(); err != nil {
		if pqError, ok := err.(*pq.Error); ok && pqError.Code == querier.SystemPQErrorCodeUndefinedFile {
			if err := h.message.Append(hypoPGExceptionMessage); err != nil {
				return errors.Wrap(err, "failed to publish message")
			}

			return nil
		}

		return errors.Wrap(err, "failed to init extension")
	}

	switch hypoSub {
	case hypoCreate:
		return h.create()

	case hypoDesc:
		return h.describe(commandTail)

	case hypoDrop:
		return h.drop(commandTail)

	case hypoReset:
		return h.reset()
	}

	return errors.New("invalid args given for the `hypo` command")
}

func (h *HypoCmd) parseQuery() (string, string) {
	const splitParts = 2

	parts := strings.SplitN(h.apiCommand.Query, " ", splitParts)

	hypoSubcommand := strings.ToLower(parts[0])

	if len(parts) < splitParts {
		return hypoSubcommand, ""
	}

	return hypoSubcommand, parts[1]
}

func (h *HypoCmd) initExtension() error {
	return querier.DBExec(h.db, "create extension if not exists hypopg")
}

func (h *HypoCmd) create() error {
	res, err := querier.DBQuery(h.db, "select * from hypopg_create_index($1)", h.apiCommand.Query)
	if err != nil {
		return errors.Wrap(err, "failed to run creation query")
	}

	tableString := &strings.Builder{}
	tableString.WriteString(HypoPGCaption)
	querier.RenderTable(tableString, res)

	if err := h.message.Append(tableString.String()); err != nil {
		return errors.Wrap(err, "failed to publish message")
	}

	return nil
}

func (h *HypoCmd) describe(indexID string) error {
	query := "select * from hypopg_list_indexes()"
	queryArgs := []interface{}{}

	if indexID != "" {
		query = `select indexrelid, indexname, hypopg_get_indexdef(indexrelid), 
			pg_size_pretty(hypopg_relation_size(indexrelid)) 
			from hypopg_list_indexes() where indexrelid = $1`
		queryArgs = append(queryArgs, indexID)
	}

	res, err := querier.DBQuery(h.db, query, queryArgs...)
	if err != nil {
		return errors.Wrap(err, "failed to run description query")
	}

	tableString := &strings.Builder{}
	tableString.WriteString(HypoPGCaption)
	querier.RenderTable(tableString, res)

	if err := h.message.Append(tableString.String()); err != nil {
		return errors.Wrap(err, "failed to publish message")
	}

	return nil
}

func (h *HypoCmd) drop(indexID string) error {
	if indexID == "" {
		return errors.Errorf("failed to drop a hypothetical index: indexrelid required")
	}

	_, err := querier.DBQuery(h.db, "select * from hypopg_drop_index($1)", indexID)
	if err != nil {
		return errors.Wrap(err, "failed to drop index")
	}

	return nil
}

func (h *HypoCmd) reset() error {
	err := querier.DBExec(h.db, "select * from hypopg_reset()")
	if err != nil {
		return errors.Wrap(err, "failed to reset indexes")
	}

	return nil
}
