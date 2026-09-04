package witness

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHelperAttribution is the check for one rule: a failure or log line
// produced by an observer must point at the line of the witness call in the
// user's code, not at a frame inside witness or core.
//
// testing.TB.Helper marks the function that calls it, so the mark has to be
// written in every frame between the test and the observer — an entry point,
// core.Context.Observe, ObserveLinked, instance. A wrapper method that calls
// tb.Helper() on the caller's behalf marks only itself, which is what this
// package used to do: every event was attributed to core/context.go.
//
// It cannot be asserted in-process — testing.TB is unimplementable outside
// the testing package and the helper set is unexported — so it runs `go test`
// on internal/helperprobe and reads the attribution back out of the output.
func TestHelperAttribution(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test in a subprocess")
	}

	out, err := exec.Command("go", "test", "-count=1", "-v",
		"-run", "TestAttributionProbe", "./internal/helperprobe").CombinedOutput()
	require.NoError(t, err, "probe package must build and pass:\n%s", out)

	// Lines look like "    probe_test.go:34: PROBE log:info".
	var line = regexp.MustCompile(`(\S+\.go):\d+: PROBE (\S+)`)
	var matches = line.FindAllStringSubmatch(string(out), -1)
	require.NotEmpty(t, matches, "probe produced no events:\n%s", out)

	var seen []string
	for _, m := range matches {
		seen = append(seen, m[2])
		require.Equal(t, "probe_test.go", m[1],
			"event %s is attributed to %s: a frame between the probe and the observer "+
				"does not call tb.Helper() in its own body", m[2], m[1])
	}
	// Every shape the probe emits, so a regression in one constructor is not
	// hidden by the others passing.
	require.Len(t, seen, 11, "probe emitted %v", strings.Join(seen, " "))
}
