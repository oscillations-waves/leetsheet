package pkg

import "fmt"

func TwoSum(nums []int, target int) []int {
	seen := make(map[int]int)
	for i, num := range nums {
		complement := target - num
		if idx, ok := seen[complement]; ok {
			return []int{idx, i}
		}
		seen[num] = i
	}
	return nil
}

func RunTwoSum() {
	fmt.Println("\n=== Two Sum ===")
	n := []int{2, 7, 11, 15}
	target := 9
	result := TwoSum(n, target)
	fmt.Printf("Array: %v, Target: %d\n", n, target)
	fmt.Printf("Result: [%d, %d]\n", result[0], result[1])
}
