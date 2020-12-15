package main

import (
	"fmt"
	"reflect"
)

func main() {
	type S struct {
		F string
	}
	x := S{}
	st := reflect.ValueOf(x)
	fmt.Printf("%s\n", st.FieldByName("F").IsValid())

}
