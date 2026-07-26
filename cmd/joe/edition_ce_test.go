//go:build !ee

/*
2026 © Postgres.ai
*/

package main

// Expected enterprise options in a community build. The community provider
// ignores the config bytes entirely and returns the constants declared in
// features/edition/ce/options, so the placeholders in testConfigYAML are
// resolved by LoadFile and then discarded.
const (
	wantQuotaLimit    = 10
	wantQuotaInterval = 60
	wantAuditEnabled  = false
	wantDBLabLimit    = 1
)
