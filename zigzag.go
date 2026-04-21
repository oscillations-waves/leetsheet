package main

import "fmt"

/*
 * Mathematical Solution: Use cycle length = 2*numRows - 2
 * For each position i, row = i % cycle if i % cycle < numRows, else cycle - (i % cycle)
 * Space complexity: O(n)
 * Time complexity: O(n)
 */

func convert(s string, numRows int) string {
	if numRows == 1 {
		return s
	}
	cycle := 2*numRows - 2 // 4
	rows := make([]string, numRows)
	for i := 0; i < len(s); i++ {
		mod := i % cycle // 0 1 5%4=1
		var row int
		if mod < numRows { // 0 < 3, 1 < 3, ..,3<3
			row = mod // 0 1
		} else {
			row = cycle - mod // 4-3=1
		}
		rows[row] += string(s[i]) //rows[0] = row[0] + P = P, row[1] = A,
		fmt.Println(rows)

	}
	ret := ""
	for _, r := range rows {
		ret += r
		fmt.Println(ret)
	}
	return ret
}
