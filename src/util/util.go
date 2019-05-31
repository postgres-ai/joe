/*
2019 © Anatoly Stansler anatoly@postgres.ai
2019 © Postgres.ai
*/

package util

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

	return true
}
