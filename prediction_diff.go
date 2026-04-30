package main

import (
	"encoding/json"
	"fmt"
	"sort"
)

func getPredictionDiff(pred1, pred2 string) []string {
	map1 := make(map[string]string)
	map2 := make(map[string]string)

	json.Unmarshal([]byte(pred1), &map1)
	json.Unmarshal([]byte(pred2), &map2)

	var result []string

	for key, val1 := range map1 {
		if val2, exists := map2[key]; exists {
			if val1 != val2 {
				result = append(result, key)
			}

		}
	}

	sort.Strings(result)
	return result
}

func main() {
	p1 := `{"hello":"world", "hi":"world", "konnichiwa":"sayonara"}`
	p2 := `{"hello":"world", "hi":"fanny", "konnichiwa":"sayonar"}`
	fmt.Println(getPredictionDiff(p1, p2))
}
