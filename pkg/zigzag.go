package pkg

import "fmt"

// Convert converts string to zigzag pattern
func Convert(s string, numRows int) string {
	if numRows == 1 {
		return s
	}
	cycle := 2*numRows - 2
	rows := make([]string, numRows)
	for i := 0; i < len(s); i++ {
		mod := i % cycle
		var row int
		if mod < numRows {
			row = mod
		} else {
			row = cycle - mod
		}
		rows[row] += string(s[i])
	}
	ret := ""
	for _, r := range rows {
		ret += r
	}
	return ret
}

func RunZigzag() {
	fmt.Println("\n=== Zigzag Conversion ===")
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
		result := Convert(tc.s, tc.numRows)
		status := "✓"
		if result != tc.expected {
			status = "✗"
		}
		fmt.Printf("%s Input: %q, numRows: %d, Expected: %q, Got: %q\n",
			status, tc.s, tc.numRows, tc.expected, result)
	}
}
