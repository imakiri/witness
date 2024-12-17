package witness

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCaller(t *testing.T) {
	var name, at = caller(0, 0)
	fmt.Println(name, at)
	require.EqualValues(t, "github.com/imakiri/witness.TestCaller", name)

	var i = testFoo(t, 4)
	_ = i
}

func testFoo(t *testing.T, i int) int {
	defer func() {
		func() {
			var name, at = caller(1, 0)
			fmt.Println(name, at)
			require.EqualValues(t, "github.com/imakiri/witness.testFoo", name)
			name, at = caller(1, 1)
			fmt.Println(name, at)
			require.EqualValues(t, "github.com/imakiri/witness.TestCaller", name)
		}()
		require.EqualValues(t, 1, 1)
	}()
	require.EqualValues(t, 1, 1)
	i *= i
	return i
}
