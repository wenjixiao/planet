package main

import (
	"testing"
	"fmt"
)

func TestInterface(t *testing.T) {
	idpool := InitIdPool(10)
	fmt.Printf("id pool:%v\n",idpool)
}