package witness

import (
	"strconv"

	"github.com/imakiri/witness/core"
)

// record is the package's own minimal core.Record, used for the few values
// witness itself attaches — an instance's version, an error's text. The
// public toolkit is the separate github.com/imakiri/witness/record module;
// this stays unexported because witness importing that module would close a
// cycle (it imports core for the Record interface).
type record struct {
	key   string
	value string
}

var _ core.Record = record{}

func (r record) AppendKey(dst []byte) []byte {
	return append(dst, r.key...)
}

func (r record) AppendValue(dst []byte) []byte {
	return append(dst, r.value...)
}

func (r record) KeyEqual(target string) bool {
	return r.key == target
}

// valueRecord is the "value" record every metric event carries: the counter
// delta, the gauge's current value, the sample. It holds the float rather
// than a formatted string so the number is rendered straight into the
// caller's buffer, like every other Record.
type valueRecord struct {
	value float64
}

var _ core.Record = valueRecord{}

func (r valueRecord) AppendKey(dst []byte) []byte {
	return append(dst, "value"...)
}

func (r valueRecord) AppendValue(dst []byte) []byte {
	return strconv.AppendFloat(dst, r.value, 'g', -1, 64)
}

func (r valueRecord) KeyEqual(target string) bool {
	return target == "value"
}

// prependValue puts the metric's value first, so an observer taking the
// first record keyed "value" sees the one the API was called with rather
// than one the caller happened to pass as a label.
func prependValue(records []core.Record, value float64) []core.Record {
	return append([]core.Record{valueRecord{value: value}}, records...)
}
