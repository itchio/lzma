package main

import "fmt"


func f1(i int) {
	defer fmt.Println(i + 1)
	fmt.Println(i + 2)
	fmt.Println(i + 3)
}

func main() { f1(3) }
