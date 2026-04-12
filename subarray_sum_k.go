package main

import "fmt"

// subarraySum returns the number of subarrays with sum equal to k.
// Uses prefix sums with a hashmap for O(n) time complexity.
func subarraySum(nums []int, k int) int {
	count := 0
	sum := 0
	sumFreq := make(map[int]int)
	sumFreq[0] = 1 // for subarrays starting from index 0

	for _, num := range nums {
		sum += num
		if freq, exists := sumFreq[sum-k]; exists {
			count += freq
		}
		sumFreq[sum]++
	}

	return count
}

func main() {
	testCases := []struct {
		nums     []int
		k        int
		expected int
	}{
		{[]int{1, 1, 1}, 2, 2},           // [1,1] at indices 0-1 and 1-2
		{[]int{1, 2, 3}, 3, 2},           // [3] and [1,2]
		{[]int{1}, 0, 0},                 // no subarray sums to 0
		{[]int{-1, -1, 1}, 0, 1},         // [-1,-1,1] sums to -1
		{[]int{1, -1, 0}, 0, 3},          // [1,-1], [1,-1,0], [-1,0]
	}

	fmt.Println("Subarray Sum Equals K:")
	fmt.Println("----------------------")

	for i, tc := range testCases {
		result := subarraySum(tc.nums, tc.k)
		status := "✓"
		if result != tc.expected {
			status = "✗"
		}
		fmt.Printf("Test %d: %s nums=%v k=%d expected=%d got=%d\n",
			i+1, status, tc.nums, tc.k, tc.expected, result)
	}
}