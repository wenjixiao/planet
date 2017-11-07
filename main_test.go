package main

import (
	"testing"
	"reflect"
	"fmt"
)

type Eater interface {
	Eat() string
}

type Cat struct {
}

func (c Cat) Eat() string {
	return "mouse*"
}

func TestInterface(t *testing.T) {
	var n int32 = 3
	tp := reflect.TypeOf(n)
	fmt.Printf("person=%v\n",tp)		
}