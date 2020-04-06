/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"strings"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/pkg/bot/querier"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
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

// hypoPGExceptionMessage defines an error message on failure of extension initialize.
const hypoPGExceptionMessage = `:warning: Cannot init the HypoPG extension.
Make sure that the extension has been installed in your Postgres image for Database Lab: https://postgres.ai/docs/database-lab/supported_databases.
For a quick start, you can use prepared images: https://hub.docker.com/repository/docker/postgresai/extended-postgres created by *Postgres.ai*, or prepare your own.`

// HypoCmd defines a hypo command.
type HypoCmd struct {
	command   *platform.Command
	message   *models.Message
	db        *pgxpool.Pool
	messenger connection.Messenger
}

// NewHypo creates a new Hypo command.
func NewHypo(cmd *platform.Command, msg *models.Message, db *pgxpool.Pool, msgSvc connection.Messenger) *HypoCmd {
	return &HypoCmd{
		command:   cmd,
		message:   msg,
		db:        db,
		messenger: msgSvc,
	}
}

// Execute runs the hypo command.
func (h *HypoCmd) Execute() error {
	hypoSub, commandTail := h.parseQuery()

	ctx := context.TODO()

	if err := h.initExtension(ctx); err != nil {
		if pgError, ok := err.(*pgconn.PgError); ok && pgError.Code == querier.SystemPQErrorCodeUndefinedFile {
			h.message.AppendText(hypoPGExceptionMessage)

			if err := h.messenger.UpdateText(h.message); err != nil {
				return errors.Wrap(err, "failed to publish message")
			}

			return nil
		}

		return errors.Wrap(err, "failed to init extension")
	}

	switch hypoSub {
	case hypoCreate:
		return h.create(ctx)

	case hypoDesc:
		return h.describe(ctx, commandTail)

	case hypoDrop:
		return h.drop(ctx, commandTail)

	case hypoReset:
		return h.reset(ctx)
	}

	return errors.New("invalid args given for the `hypo` command")
}

func (h *HypoCmd) parseQuery() (string, string) {
	const splitParts = 2

	parts := strings.SplitN(h.command.Query, " ", splitParts)

	hypoSubcommand := strings.ToLower(parts[0])

	if len(parts) < splitParts {
		return hypoSubcommand, ""
	}

	return hypoSubcommand, parts[1]
}

func (h *HypoCmd) initExtension(ctx context.Context) error {
	_, err := h.db.Exec(ctx, "create extension if not exists hypopg")

	return err
}

func (h *HypoCmd) create(ctx context.Context) error {
	res, err := querier.DBQuery(ctx, h.db, "select indexrelid::text, indexname from hypopg_create_index($1)", h.command.Query)
	if err != nil {
		return errors.Wrap(err, "failed to run creation query")
	}

	tableString := &strings.Builder{}
	tableString.WriteString(HypoPGCaption)
	querier.RenderTable(tableString, res)
	h.message.AppendText(tableString.String())

	if err := h.messenger.UpdateText(h.message); err != nil {
		return errors.Wrap(err, "failed to publish message")
	}

	return nil
}

func (h *HypoCmd) describe(ctx context.Context, indexID string) error {
	query := "select indexrelid::text, indexname, nspname, relname, amname from hypopg_list_indexes()"
	queryArgs := []interface{}{}

	if indexID != "" {
		query = `select indexrelid::text, indexname, hypopg_get_indexdef(indexrelid), 
			pg_size_pretty(hypopg_relation_size(indexrelid)) 
			from hypopg_list_indexes() where indexrelid = $1`
		queryArgs = append(queryArgs, indexID)
	}

	res, err := querier.DBQuery(ctx, h.db, query, queryArgs...)
	if err != nil {
		return errors.Wrap(err, "failed to run description query")
	}

	tableString := &strings.Builder{}
	tableString.WriteString(HypoPGCaption)
	querier.RenderTable(tableString, res)

	h.message.AppendText(tableString.String())
	if err := h.messenger.UpdateText(h.message); err != nil {
		return errors.Wrap(err, "failed to publish message")
	}

	return nil
}

func (h *HypoCmd) drop(ctx context.Context, indexID string) error {
	if indexID == "" {
		return errors.Errorf("failed to drop a hypothetical index: indexrelid required")
	}

	_, err := querier.DBQuery(ctx, h.db, "select * from hypopg_drop_index($1)", indexID)
	if err != nil {
		return errors.Wrap(err, "failed to drop index")
	}

	return nil
}

func (h *HypoCmd) reset(ctx context.Context) error {
	if _, err := h.db.Exec(ctx, "select * from hypopg_reset()"); err != nil {
		return errors.Wrap(err, "failed to reset indexes")
	}

	return nil
}
