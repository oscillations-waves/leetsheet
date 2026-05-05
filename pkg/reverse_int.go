package pkg

import "fmt"

// Reverse reverses a 32-bit signed integer
func Reverse(x int) int {
	rev := 0
	INT_MAX := 2147483647
	INT_MIN := -2147483648

	for x != 0 {
		pop := x % 10
		x /= 10

		if rev > INT_MAX/10 || (rev == INT_MAX/10 && pop > 7) {
			return 0
		}
		if rev < INT_MIN/10 || (rev == INT_MIN/10 && pop < -8) {
			return 0
		}

		rev = rev*10 + pop
	}

	return rev
}

func RunReverseInt() {
	fmt.Println("\n=== Reverse Int ===")
	testCases := []struct {
		input    int
		expected int
	}{
		{123, 321},
		{-123, -321},
		{120, 21},
		{0, 0},
		{1534236469, 0}, // overflow
	}

	for _, tc := range testCases {
		result := Reverse(tc.input)
		status := "✓"
		if result != tc.expected {
			status = "✗"
		}
		fmt.Printf("%s Input: %d, Expected: %d, Got: %d\n", status, tc.input, tc.expected, result)
	}
}
