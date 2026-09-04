package witness

import "github.com/imakiri/witness/core"

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
