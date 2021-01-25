/*
2021 © Postgres.ai
*/

package estimator

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimateTiming(t *testing.T) {
	waitEvents := map[string]float64{
		"Running":                  45.63,
		"IO.DataFileRead":          17.60,
		"IO.WALSync":               17.00,
		"IO.DataFileImmediateSync": 10.97,
		"IO.BufFileRead":           2.21,
		"IO.BufFileWrite":          2.20,
		"IO.DataFileExtend":        2.20,
		"IO.WALWrite":              2.19,
	}

	const (
		readFactor   = 1.2
		writeFactor  = 1.2
		cloneTiming  = 9.53
		expectedTime = 7.09
	)

	est := NewTiming(waitEvents, readFactor, writeFactor)

	estimatedTime := est.CalcMin(cloneTiming)
	assert.Equal(t, expectedTime, math.Round(estimatedTime*100)/100)
}
