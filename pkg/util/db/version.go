// Package db contains database helpers.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v4"
)

const dbVersionQuery = `select setting::integer/10000 from pg_settings where name = 'server_version_num'`

// GetMajorVersion returns the major Postgres version.
func GetMajorVersion(ctx context.Context, conn *pgx.Conn) (int, error) {
	var majorVersion int

	row := conn.QueryRow(ctx, dbVersionQuery)

	if err := row.Scan(&majorVersion); err != nil {
		return 0, fmt.Errorf("failed to perform query detecting major version: %w", err)
	}

	return majorVersion, nil
}
