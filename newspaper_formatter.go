package main

import "strings"

// Newspaper Page Formatter
// Format paragraphs into a fixed-width newspaper page with centering and * border.

// paragraphs = [["hello","world"], ["How","areYou","doing"],
//               ["Please look","and align","to center"]]
// width = 16

// Output:
// "******************"
// "*  hello world   *"   ← 5 leftover, odd → 2 leading, 3 trailing
// "*How areYou doing*"   ← 0 leftover, fits exactly
// "*  Please look   *"   ← 5 leftover, odd → 2 leading, 3 trailing
// "*   and align    *"   ← 7 leftover, odd → 3 leading, 4 trailing
// "*   to center    *"   ← 7 leftover, odd → 3 leading, 4 trailing
// "******************"

func formatNewspaper(paragraphs [][]string, width int) []string {
	border := strings.Repeat("*", width+2)
	result := []string{border}

	for _, para := range paragraphs {
		// Word-wrap words in this paragraph to fit within width.
		var lines []string
		current := ""
		for _, word := range para {
			switch {
			case current == "":
				current = word
			case len(current)+1+len(word) <= width:
				current += " " + word
			default:
				lines = append(lines, current)
				current = word
			}
		}
		if current != "" {
			lines = append(lines, current)
		}

		// Center each wrapped line and surround with * borders.
		for _, line := range lines {
			leftover := width - len(line)
			leading := leftover / 2
			trailing := leftover - leading
			result = append(result, "*"+strings.Repeat(" ", leading)+line+strings.Repeat(" ", trailing)+"*")
		}
	}

	return append(result, border)
}
