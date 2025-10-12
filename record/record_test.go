package record

import (
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
	var buf []byte
	for _, record := range records {
		buf = record.AppendKey(buf)
		buf = record.AppendValue(buf)
	}

	const expected = `test.Foo 12
test.Bar fgh
test.Buzz true
test.testStruct2.A 123
test.testStruct2.T[0] 8
test.testStruct2.T[1] 98
`
	_, _ = os.Stdout.Write(buf)
	require.Equal(t, expected, string(buf))
}
