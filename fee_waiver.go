package main

import "fmt"

// monthlyFee computes total yearly fee for transactions grouped by month.
// Each month has a base fee of 5, waived only if both conditions apply:
// - at least 3 negative transactions (card payments)
// - absolute sum of negative transactions >= 100
func monthlyFee(A [][]int) int {
	if A == nil {
		return 0
	}

	total := 0
	for month, txs := range A {
		fee := 5
		negCount := 0
		negSum := 0

		for _, v := range txs {
			if v < 0 {
				negCount++
				negSum += -v
			}
		}

		if negCount >= 3 && negSum >= 100 {
			fee = 0
		}

		fmt.Printf("Month %d: negPayments=%d, negSum=%d, fee=%d\n", month+1, negCount, negSum, fee)
		total += fee
	}

	return total
}

func main() {
	months := [][]int{
		{+120, -40, -35, -30, +15},    // month 1: 3 negatives, sum=105 -> waived
		{-20, -20, -20, +10},          // month 2: 3 negatives, sum=60 -> fee applies
		{-50, -25, -30, -10, +100},    // month 3: 4 negatives, sum=115 -> waived
		{+200, -10, -5},               // month 4: 2 negatives -> fee applies
	}

	result := monthlyFee(months)
	fmt.Printf("Total yearly fee: %d\n", result)
}
