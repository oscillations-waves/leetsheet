package main

func reverse(x int) int {
	rev := 0
	INT_MAX := 2147483647
	INT_MIN := -2147483648

	for x != 0 {
		pop := x % 10
		x /= 10

		// Check overflow before it happens
		if rev > INT_MAX/10 || (rev == INT_MAX/10 && pop > 7) {
			return 0
		}
		if rev < INT_MIN/10 || (rev == INT_MIN/10 && pop < -8) {
			return 0
		}

		rev = rev*10 + pop
	}

	return rev
}
func main() {
	x := 123
	result := reverse(x)
	println(result) // Output: 321
}
