package pkg

import "fmt"

// CaseDiff computes the difference between uppercase and lowercase letters
func CaseDiff(typedText string) int {
	upper, lower := 0, 0
	for _, ch := range typedText {
		if ch >= 'A' && ch <= 'Z' {
			upper++
		} else {
			lower++
		}
	}
	return upper - lower
}

func RunCaseDiff() {
	fmt.Println("\n=== Case Diff ===")
	testCases := []struct {
		input    string
		expected int
	}{
		{"CodeSignal", -6},
		{"a", -1},
		{"AbCdEf", 0},
	}

	for _, tc := range testCases {
		result := CaseDiff(tc.input)
		status := "✓"
		if result != tc.expected {
			status = "✗"
		}
		fmt.Printf("%s Input: %q, Expected: %d, Got: %d\n", status, tc.input, tc.expected, result)
	}
}
