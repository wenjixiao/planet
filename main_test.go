package main

import (
	"testing"
	"fmt"
)

type Printer interface {
	Print()
}

type Person struct {
	Name string
	Age byte
}

func (p Person) Print() {
	fmt.Printf("name=%v,age=%v\n",p.Name,p.Age)
}

type Student struct {
	Person
	Code string
}

func (s Student) Print(){
	fmt.Printf("name=%v,age=%v,code=%v\n",s.Person.Name,s.Person.Age,s.Code)
}

func show(p Printer) {
	p.Print()
}

func TestInterface(t *testing.T) {
	var p Printer
	p = Student{Person{"wenjixiao",40},"9527"}
	show(p)
}