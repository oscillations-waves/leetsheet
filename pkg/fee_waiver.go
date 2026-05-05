package pkg

import "fmt"

// MonthlyFee computes total yearly fee for transactions
func MonthlyFee(A [][]int) int {
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

func RunFeeWaiver() {
	fmt.Println("\n=== Fee Waiver ===")
	months := [][]int{
		{+120, -40, -35, -30, +15},
		{-20, -20, -20, +10},
		{-50, -25, -30, -10, +100},
		{+200, -10, -5},
	}

	result := MonthlyFee(months)
	fmt.Printf("Total yearly fee: %d\n", result)
}
