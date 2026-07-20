package pkg

import "fmt"

// ReverseArray reverses a slice of integers in-place using two pointers.
// Time: O(n)  Space: O(1)
func ReverseArray(nums []int) {
	left, right := 0, len(nums)-1
	for left < right {
		nums[left], nums[right] = nums[right], nums[left]
		left++
		right--
	}
}

func RunReverseArray() {
	fmt.Println("\n=== Reverse Array ===")
	testCases := []struct {
		input    []int
		expected []int
	}{
		{[]int{1, 2, 3, 4, 5}, []int{5, 4, 3, 2, 1}},
		{[]int{1, 2, 3, 4}, []int{4, 3, 2, 1}},
		{[]int{42}, []int{42}},
		{[]int{}, []int{}},
	}

	for _, tc := range testCases {
		original := make([]int, len(tc.input))
		copy(original, tc.input)
		ReverseArray(tc.input)
		fmt.Printf("Input: %v → Reversed: %v\n", original, tc.input)
	}
}
