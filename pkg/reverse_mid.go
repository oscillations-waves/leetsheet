package pkg

import "fmt"

// ReverseMiddle reverses the middle of words bounded by vowels
func ReverseMiddle(text []string) []string {
	for i, word := range text {
		n := len(word)
		if isVowel(word[0]) && isVowel(word[n-1]) {
			b := []byte(word)
			left, right := 1, n-2
			for left < right {
				b[left], b[right] = b[right], b[left]
				left++
				right--
			}
			text[i] = string(b)
		}
	}
	return text
}

func isVowel(c byte) bool {
	switch c {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return true
	}
	return false
}

func RunReverseMid() {
	fmt.Println("\n=== Reverse Mid ===")
	text1 := []string{"apple", "banana", "OranGe"}
	text2 := []string{"AE", "CodeSignal"}

	result := ReverseMiddle(text1)
	fmt.Printf("Result 1: %v\n", result)

	result = ReverseMiddle(text2)
	fmt.Printf("Result 2: %v\n", result)
}
