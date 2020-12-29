/*
2019 © Postgres.ai
*/

package util

import (
	"fmt"
	"math"
	"time"
)

const (
	// NA defines the "not available" placeholder.
	NA = "N/A"
)

func EqualStringSlicesUnordered(x, y []string) bool {
	xMap := make(map[string]int)
	yMap := make(map[string]int)

	for _, xElem := range x {
		xMap[xElem]++
	}
	for _, yElem := range y {
		yMap[yElem]++
	}

	for xMapKey, xMapVal := range xMap {
		if yMap[xMapKey] != xMapVal {
			return false
		}
	}

	for yMapKey, yMapVal := range yMap {
		if xMap[yMapKey] != yMapVal {
			return false
		}
	}

	return true
}

func Contains(list []string, s string) bool {
	for _, item := range list {
		if s == item {
			return true
		}
	}
	return false
}

func SecondsAgo(ts time.Time) uint {
	now := time.Now()
	return uint(math.Floor(now.Sub(ts).Seconds()))
}

func MinutesAgo(ts time.Time) uint {
	now := time.Now()
	return uint(math.Floor(now.Sub(ts).Minutes()))
}

func RunInterval(d time.Duration, fn func()) chan struct{} {
	ticker := time.NewTicker(d)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				fn()
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	// Use `close(quit)` to stop interval execution.
	return quit
}

func DurationToString(value time.Duration) string {
	return MillisecondsToString(float64(value / 1000000))
}

func MillisecondsToString(value float64) string {
	if value < 1000 {
		return fmt.Sprintf("%.3f ms", value)
	} else if value < 60000 {
		return fmt.Sprintf("%.3f s", value/1000.0)
	} else {
		return fmt.Sprintf("%.3f min", value/60000.0)
	}
}
