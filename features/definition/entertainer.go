/*
2019 © Postgres.ai
*/

package definition

// Entertainer provides a help message for Enterprise commands.
type Entertainer interface {
	GetEdition() string
	GetEnterpriseHelpMessage() string
}
