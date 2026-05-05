package pkg

import "fmt"

func BinaryGap(n int) int {
	maxGap, currentGap := 0, 0
	foundOne := false
	for n > 0 {
		if n%2 == 1 {
			if foundOne && currentGap > maxGap {
				maxGap = currentGap
			}
			foundOne = true
			currentGap = 0
		} else {
			if foundOne {
				currentGap++
			}
		}
		n = n / 2
	}
	return maxGap
}

func RunBinaryGap() {
	fmt.Println("\n=== Binary Gap ===")
	testCases := []struct {
		input    int
		expected int
	}{
		{5, 1},      // binary: 101, one zero between 1's
		{8, 0},      // binary: 1000, only one 1, so 0
		{6, 0},      // binary: 110, no zeros between 1's
		{9, 2},      // binary: 1001, two zeros between 1's
		{22, 1},     // binary: 10110, gaps are 1 and 0, max is 1
		{1, 0},      // binary: 1, only one 1, so 0
		{1024, 0},   // binary: 10000000000, only one 1, so 0
		{529, 4},    // binary: 1000010001, longest gap is 4
		{1041, 5},   // binary: 10000010001, longest gap is 5
	}

	for _, tc := range testCases {
		result := BinaryGap(tc.input)
		status := "✓"
		if result != tc.expected {
			status = "✗"
		}
		fmt.Printf("%s Input: %d (binary: %b), Expected: %d, Got: %d\n",
			status, tc.input, tc.input, tc.expected, result)
	}
}
