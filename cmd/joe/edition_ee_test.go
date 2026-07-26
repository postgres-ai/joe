//go:build ee

/*
2026 © Postgres.ai
*/

package main

// Expected enterprise options in an enterprise build. The enterprise provider
// re-parses the expanded bytes, so these are the values setupTestEnv exports
// into EE_TEST_*. Every one of them is a number or a boolean, which is what
// made the enterprise subtree the place an unquoted placeholder used to abort
// startup.
const (
	wantQuotaLimit    = 20
	wantQuotaInterval = 120
	wantAuditEnabled  = true
	wantDBLabLimit    = 3
)
