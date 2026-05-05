package pkg

import "fmt"

// MinDeletionsToSorted returns minimum deletions to make array non-decreasing
func MinDeletionsToSorted(A []int) int {
	if len(A) == 0 {
		return 0
	}

	tails := []int{}

	for _, x := range A {
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

func RunMinDeletionsSorted() {
	fmt.Println("\n=== Min Deletions Sorted ===")
	cases := []struct {
		input    []int
		expected int
	}{
		{[]int{5, 1, 3, 2, 6}, 2},
		{[]int{1, 2, 3, 4, 5}, 0},
		{[]int{5, 4, 3, 2, 1}, 4},
		{[]int{2, 2, 2, 2}, 0},
		{[]int{}, 0},
	}

	for _, tc := range cases {
		res := MinDeletionsToSorted(tc.input)
		status := "✓"
		if res != tc.expected {
			status = "✗"
		}
		fmt.Printf("%s input=%v expected=%d got=%d\n", status, tc.input, tc.expected, res)
	}
}
