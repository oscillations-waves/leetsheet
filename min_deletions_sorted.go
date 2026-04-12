package main

import "fmt"

// minDeletionsToSorted returns the minimum deletions to make A non-decreasing.
// This is len(A) - length of longest non-decreasing subsequence (LNDS).
func minDeletionsToSorted(A []int) int {
	if len(A) == 0 {
		return 0
	}

	// tails[i] = smallest tail value of a non-decreasing subsequence length i+1
	tails := []int{}

	for _, x := range A {
		// binary search for first position where tails[pos] > x (strictly greater)
		// to allow non-decreasing, we replace first strictly greater value.
		lo, hi := 0, len(tails)
		for lo < hi {
			mid := (lo + hi) / 2
			if tails[mid] > x {
				hi = mid
			} else {
				lo = mid + 1
			}
		}

		if lo == len(tails) {
			tails = append(tails, x)
		} else {
			tails[lo] = x
		}
	}

	lnds := len(tails)
	return len(A) - lnds
}

func main() {
	cases := []struct {
		input    []int
		expected int
	}{
		{[]int{5, 1, 3, 2, 6}, 2},   // keep [1,3,6] or [1,2,6]
		{[]int{1, 2, 3, 4, 5}, 0},   // already non-decreasing
		{[]int{5, 4, 3, 2, 1}, 4},   // best keep 1 element
		{[]int{2, 2, 2, 2}, 0},     // non-decreasing
		{[]int{}, 0},
	}

	for _, tc := range cases {
		res := minDeletionsToSorted(tc.input)
		status := "✓"
		if res != tc.expected {
			status = "✗"
		}
		fmt.Printf("%s input=%v expected=%d got=%d\n", status, tc.input, tc.expected, res)
	}
}
