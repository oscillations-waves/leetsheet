package pkg

import "fmt"

// SubarraySum returns the number of subarrays with sum equal to k
func SubarraySum(nums []int, k int) int {
	count := 0
	sum := 0
	sumFreq := make(map[int]int)
	sumFreq[0] = 1

	for _, num := range nums {
		sum += num
		if freq, exists := sumFreq[sum-k]; exists {
			count += freq
		}
		sumFreq[sum]++
	}

	return count
}

func RunSubarraySumK() {
	fmt.Println("\n=== Subarray Sum K ===")
	testCases := []struct {
		nums     []int
		k        int
		expected int
	}{
		{[]int{1, 1, 1}, 2, 2},
		{[]int{1, 2, 3}, 3, 2},
		{[]int{1}, 0, 0},
		{[]int{-1, -1, 1}, 0, 1},
		{[]int{1, -1, 0}, 0, 3},
	}

	fmt.Println("Subarray Sum Equals K:")
	fmt.Println("----------------------")

	for i, tc := range testCases {
		result := SubarraySum(tc.nums, tc.k)
		status := "✓"
		if result != tc.expected {
			status = "✗"
		}
		fmt.Printf("Test %d: %s nums=%v k=%d expected=%d got=%d\n",
			i+1, status, tc.nums, tc.k, tc.expected, result)
	}
}
