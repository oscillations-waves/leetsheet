package pkg

import (
	"encoding/json"
	"fmt"
	"sort"
)

// GetPredictionDiff returns keys with different values between two JSON objects
func GetPredictionDiff(pred1, pred2 string) []string {
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

func RunPredictionDiff() {
	fmt.Println("\n=== Prediction Diff ===")
	p1 := `{"hello":"world", "hi":"world", "konnichiwa":"sayonara"}`
	p2 := `{"hello":"world", "hi":"fanny", "konnichiwa":"sayonar"}`
	fmt.Printf("Prediction 1: %s\n", p1)
	fmt.Printf("Prediction 2: %s\n", p2)
	fmt.Printf("Differences: %v\n", GetPredictionDiff(p1, p2))
}
