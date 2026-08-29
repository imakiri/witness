package record

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test(t *testing.T) {
	var marshaller = Marshaller[DefaultKeyJoiner]{
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

	for _, r := range marshaller.Marshal("testStruct1", testStruct1) {
		var buf []byte
		buf = r.AppendKey(buf)
		buf = r.AppendValue(buf)
		fmt.Println(string(buf))
	}

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
	for _, r := range marshaller.Marshal("testStruct2", testStruct2) {
		var buf []byte
		buf = r.AppendKey(buf)
		buf = r.AppendValue(buf)
		fmt.Println(string(buf))
	}

}

type namedBytes []byte
type namedByte byte

// Byte slices and arrays collapse into a single base64 record. The check is
// on the element kind, so named types land here too — and an array must not
// go through v.Bytes(), which panics on an unaddressable byte array.
func TestMarshalByteSequences(t *testing.T) {
	var marshaller = Marshaller[DefaultKeyJoiner]{MaxDepth: 16}

	type TestStruct struct {
		Plain    []byte
		Named    namedBytes
		NamedEl  []namedByte
		Array    [3]byte
		NamedArr [2]namedByte
		Empty    []byte
		NotBytes []uint16
	}
	var records = marshaller.Marshal("s", TestStruct{
		Plain:    []byte("ab"),
		Named:    namedBytes("cd"),
		NamedEl:  []namedByte{'e', 'f'},
		Array:    [3]byte{'g', 'h', 'i'},
		NamedArr: [2]namedByte{'j', 'k'},
		NotBytes: []uint16{1, 2},
	})

	var got = map[string]string{}
	for _, r := range records {
		got[string(r.AppendKey(nil))] = string(r.AppendValue(nil))
	}

	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("ab")), got["s.Plain"])
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("cd")), got["s.Named"])
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("ef")), got["s.NamedEl"])
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("ghi")), got["s.Array"])
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("jk")), got["s.NamedArr"])
	require.Equal(t, "", got["s.Empty"])

	// Anything whose elements are not bytes still expands per element.
	require.NotContains(t, got, "s.NotBytes")
	require.Equal(t, "1", got["s.NotBytes[0]"])
	require.Equal(t, "2", got["s.NotBytes[1]"])
}
