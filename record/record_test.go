package record

import (
	"bytes"
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

type testStruct2 struct {
	A string
	T []int64
}

type testStruct struct {
	Foo  int
	Bar  string
	Buzz bool
	testStruct2
}

func TestStruct1(t *testing.T) {
	var s = testStruct{
		Foo:  12,
		Bar:  "fgh",
		Buzz: true,
		testStruct2: testStruct2{
			A: "123",
			T: []int64{8, 98},
		},
	}

	var marshaller = Marshaller[DefaultFormatter]{
		MaxDepth: 16,
	}

	var records = marshaller.Marshal("test", s)
	var buf = new(bytes.Buffer)
	for _, record := range records {
		fmt.Fprintln(buf, record.Name(), record.String())
	}

	const expected = `test.Foo 12
test.Bar fgh
test.Buzz true
test.testStruct2.A 123
test.testStruct2.T[0] 8
test.testStruct2.T[1] 98
`
	var actual = buf.String()
	buf.WriteTo(os.Stdout)

	require.Equal(t, expected, actual)
}
