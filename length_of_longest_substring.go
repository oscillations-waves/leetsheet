package main

func lengthOfLongestSubstring(s string) int {
	charIndex := make(map[byte]int)
	left := 0
	maxLen := 0

	for right := 0; right < len(s); right++ {
		if idx, found := charIndex[s[right]]; found && idx >= left {
			left = idx + 1
		}
		charIndex[s[right]] = right

		if currentLen := (right - left + 1); currentLen > maxLen {
			maxLen = currentLen
		}
	}
	return maxLen
}

func main() {
	s := "abcabcbb"
	result := lengthOfLongestSubstring(s)
	println(result) // Output: 3 (the longest substring is "abc")
}
