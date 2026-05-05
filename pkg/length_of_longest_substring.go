package pkg

import "fmt"

// LengthOfLongestSubstring returns the length of the longest substring without repeating chars
func LengthOfLongestSubstring(s string) int {
	charIndex := make(map[byte]int)
	left := 0
	maxLen := 0

	for right := 0; right < len(s); right++ {
		if idx, found := charIndex[s[right]]; found && idx >= left {
			left = idx + 1
		}
		charIndex[s[right]] = right

		if currentLen := (right - left + 1); currentLen > maxLen {
			maxLen = currentLen
		}
	}
	return maxLen
}

func RunLengthOfLongestSubstring() {
	fmt.Println("\n=== Length of Longest Substring ===")
	testCases := []struct {
		input    string
		expected int
	}{
		{"abcabcbb", 3},
		{"bbbbb", 1},
		{"pwwkew", 3},
		{"au", 2},
	}

	for _, tc := range testCases {
		result := LengthOfLongestSubstring(tc.input)
		status := "✓"
		if result != tc.expected {
			status = "✗"
		}
		fmt.Printf("%s Input: %q, Expected: %d, Got: %d\n", status, tc.input, tc.expected, result)
	}
}
