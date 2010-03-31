package lzma

import "fmt"


func F1(i int) {
	defer fmt.Println(i + 1)
	fmt.Println(i + 2)
	fmt.Println(i + 3)
}
