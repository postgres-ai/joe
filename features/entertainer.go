/*
2019 © Postgres.ai
*/

package features

import (
	"gitlab.com/postgres-ai/joe/features/definition"
)

// entertainerService contains the entertainer service for different editions of the application.
var entertainerService definition.Entertainer

// GetEntertainer gets an application entertainerService.
func GetEntertainer() definition.Entertainer {
	return entertainerService
}
