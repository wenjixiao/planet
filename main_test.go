package main

import (
	"testing"
	// "fmt"
)

func myswitch(n int){
	switch{
	case n==1:
		println(1)
	case n==2:
		println(2)
	}
}

func TestInterface(t *testing.T) {
	myswitch(1)
}