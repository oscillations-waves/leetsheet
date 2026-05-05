package pkg

import "fmt"

// MinOperations calculates minimum operations for stepwise pattern
func MinOperations(structures []int) int64 {
	n := len(structures)

	var baseAsc int64 = -1 << 60
	var baseDesc int64 = -1 << 60

	for i := 0; i < n; i++ {
		val := int64(structures[i]) - int64(i)
		if val > baseAsc {
			baseAsc = val
		}
	}
	var costAsc int64 = 0
	for i := 0; i < n; i++ {
		target := baseAsc + int64(i)
		costAsc += target - int64(structures[i])
	}

	for i := 0; i < n; i++ {
		val := int64(structures[i]) + int64(i)
		if val > baseDesc {
			baseDesc = val
		}
	}

	var costDesc int64 = 0
	for i := 0; i < n; i++ {
		target := baseDesc - int64(i)
		costDesc += target - int64(structures[i])
	}

	if costAsc < costDesc {
		return costAsc
	}
	return costDesc
}

func RunStepwiseStructure() {
	fmt.Println("\n=== Stepwise Structure ===")
	testCases := []struct {
		input    []int
		expected int64
	}{
		{[]int{1, 4, 3, 2}, 4},
		{[]int{5, 7, 9, 4, 11}, 9},
	}

	for _, tc := range testCases {
		result := MinOperations(tc.input)
		status := "✓"
		if result != tc.expected {
			status = "✗"
		}
		fmt.Printf("%s Input: %v, Expected: %d, Got: %d\n", status, tc.input, tc.expected, result)
	}
}
