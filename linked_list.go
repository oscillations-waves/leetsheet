package main

import "fmt"

type Node struct {
	Value int
	Next  *Node
}

type LinkedList struct {
	Head *Node
}

func (l *LinkedList) InsertFront(value int) {
	newNode := &Node{Value: value, Next: l.Head}
	l.Head = newNode

}

func (l *LinkedList) InsertBack(value int) {
	newNode := &Node{Value: value}

	if l.Head == nil {
		l.Head = newNode
	}
	current := l.Head
	for current.Next != nil {
		current = current.Next
	}
	current.Next = newNode
}

func (l *LinkedList) Delete(value int) {
	if l.Head == nil {
		return
	}
	current := l.Head
	for current.Next != nil && current.Next.Value != value {
		current = current.Next
	}
	if current.Next != nil {
		current.Next = current.Next.Next
	}
}

func (l *LinkedList) Print() {
	current := l.Head
	for current != nil {
		fmt.Println("%d ->", current.Value)
		current = current.Next
	}
	fmt.Println("nil")
}

func main() {
	list := &LinkedList{}
	list.InsertFront(3)
	list.InsertFront(2)
	list.InsertFront(1)
	list.InsertBack(4)

	list.Print() // 1 -> 2 -> 3 -> 4 -> nil

	list.Delete(2)
	list.Print() // 1 -> 3 -> 4 -> nil
}
