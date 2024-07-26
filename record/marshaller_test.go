package record

import (
	"fmt"
	"testing"
)

func Test(t *testing.T) {
	var marshaller = Marshaller[DefaultFormatter]{
		MaxDepth: 16,
	}

	type TestStruct1 struct {
		Foo  string
		Bar  int
		Buzz chan error
	}
	var testStruct1 = TestStruct1{
		Foo:  "foo",
		Bar:  7,
		Buzz: make(chan error),
	}

	fmt.Println(marshaller.Marshal("testStruct1", testStruct1))

	type TestStruct2 struct {
		TestStruct1
		A []uint
		M map[int]struct{}
	}
	var testStruct2 = TestStruct2{
		TestStruct1: testStruct1,
		A:           []uint{1, 4},
		M:           map[int]struct{}{2: {}, 7: {}},
	}

	fmt.Println(marshaller.Marshal("testStruct2", testStruct2))
}
