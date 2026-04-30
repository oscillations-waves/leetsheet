package main

func twoSum(nums []int, target int) []int {
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

func main() {
	n := []int{2, 7, 11, 15}
	target := 9
	result := twoSum(n, target)
	println(result[0], result[1]) // Output: 0 1
}
