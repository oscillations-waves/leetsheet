package main

import "fmt"

func main() {
	tests := []struct {
		s        string
		numRows  int
		expected string
	}{
		{"PAYPALISHIRING", 3, "PAHNAPLSIIGYIR"},
		{"PAYPALISHIRING", 4, "PINALSIGYAHRPI"},
		{"A", 1, "A"},
		{"AB", 1, "AB"},
	}

	for _, tc := range tests {
		result := convert(tc.s, tc.numRows)
		status := "✓"
		if result != tc.expected {
			status = "✗"
		}
		fmt.Printf("%s Input: \"%s\", numRows: %d, Expected: \"%s\", Got: \"%s\"\n",
			status, tc.s, tc.numRows, tc.expected, result)
	}
}
