package pkg

import "fmt"

type ListNode struct {
	Value int
	Next  *ListNode
}

func AddNumbers(l1, l2 *ListNode) *ListNode {
	result := &ListNode{}
	carry := 0
	current := result
	for l1 != nil || l2 != nil || carry != 0 {
		sum := carry
		if l1 != nil {
			sum = sum + l1.Value
			l1 = l1.Next
		}
		if l2 != nil {
			sum = sum + l2.Value
			l2 = l2.Next
		}
		carry = sum / 10
		current.Next = &ListNode{Value: sum % 10}
		current = current.Next
	}
	return result.Next
}

func PrintList(head *ListNode) {
	for head != nil {
		fmt.Print(head.Value)
		if head.Next != nil {
			fmt.Print(" -> ")
		}
		head = head.Next
	}
	fmt.Println()
}

func RunAddTwoNumbers() {
	fmt.Println("\n=== Add Two Numbers ===")
	// Test case 1: 342 + 465 = 807
	l1 := &ListNode{Value: 2, Next: &ListNode{Value: 4, Next: &ListNode{Value: 3}}}
	l2 := &ListNode{Value: 5, Next: &ListNode{Value: 6, Next: &ListNode{Value: 4}}}
	result := AddNumbers(l1, l2)
	fmt.Print("342 + 465 = ")
	PrintList(result)

	// Test case 2: 0 + 0 = 0
	l3 := &ListNode{Value: 0}
	l4 := &ListNode{Value: 0}
	result2 := AddNumbers(l3, l4)
	fmt.Print("0 + 0 = ")
	PrintList(result2)

	// Test case 3: 999 + 1 = 1000
	l5 := &ListNode{Value: 9, Next: &ListNode{Value: 9, Next: &ListNode{Value: 9}}}
	l6 := &ListNode{Value: 1}
	result3 := AddNumbers(l5, l6)
	fmt.Print("999 + 1 = ")
	PrintList(result3)
}
