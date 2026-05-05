package main

import (
	"flag"
	"fmt"
	"leetsheet/pkg"
	"log"
)

func main() {
	problem := flag.String("problem", "help", "Problem to run")
	flag.Parse()

	switch *problem {
	case "add-two-numbers":
		pkg.RunAddTwoNumbers()
	case "binary-gap":
		pkg.RunBinaryGap()
	case "case-diff":
		pkg.RunCaseDiff()
	case "fee-waiver":
		pkg.RunFeeWaiver()
	case "length-of-longest-substring":
		pkg.RunLengthOfLongestSubstring()
	case "linked-list":
		pkg.RunLinkedList()
	case "min-deletions-sorted":
		pkg.RunMinDeletionsSorted()
	case "prediction-diff":
		pkg.RunPredictionDiff()
	case "reverse-int":
		pkg.RunReverseInt()
	case "reverse-mid":
		pkg.RunReverseMid()
	case "stepwise-structure":
		pkg.RunStepwiseStructure()
	case "subarray-sum-k":
		pkg.RunSubarraySumK()
	case "two-sum":
		pkg.RunTwoSum()
	case "worker-pool":
		pkg.RunWorkerPool()
	case "zigzag":
		pkg.RunZigzag()
	case "help", "":
		printHelp()
	default:
		log.Fatalf("Unknown problem: %s\n", *problem)
	}
}

func printHelp() {
	fmt.Println("LeetSheet - Go LeetCode Problems Collection")
	fmt.Println("==========================================")
	fmt.Println("\nUsage: go run ./cmd -problem=<problem_name>")
	fmt.Println("\nAvailable problems:")
	fmt.Println("  add-two-numbers            - Add two numbers represented as linked lists")
	fmt.Println("  binary-gap                 - Find maximum gap between 1's in binary representation")
	fmt.Println("  case-diff                  - Compute difference between uppercase and lowercase letters")
	fmt.Println("  fee-waiver                 - Calculate monthly fees based on transactions")
	fmt.Println("  length-of-longest-substring - Find longest substring without repeating characters")
	fmt.Println("  linked-list                - Linked list operations (insert, delete, print)")
	fmt.Println("  min-deletions-sorted       - Minimum deletions to make array non-decreasing")
	fmt.Println("  prediction-diff            - Compare two JSON objects for differences")
	fmt.Println("  reverse-int                - Reverse a 32-bit signed integer")
	fmt.Println("  reverse-mid                - Reverse middle of words bounded by vowels")
	fmt.Println("  stepwise-structure         - Minimum operations for stepwise pattern")
	fmt.Println("  subarray-sum-k             - Find subarrays with sum equal to k")
	fmt.Println("  two-sum                    - Find two numbers in array that add to target")
	fmt.Println("  worker-pool                - Concurrent worker pool example")
	fmt.Println("  zigzag                     - Zigzag conversion of string")
	fmt.Println("\nExamples:")
	fmt.Println("  go run ./cmd -problem=two-sum")
	fmt.Println("  go run ./cmd -problem=worker-pool")
}
