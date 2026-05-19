package witness

type Record interface {
	AppendKey(dst []byte) []byte
	AppendValue(dst []byte) []byte
	KeyEqual(target string) bool
}

type record struct {
	key   string
	value string
}

func (r record) AppendKey(dst []byte) []byte {
	return append(dst, r.key...)
}

func (r record) AppendValue(dst []byte) []byte {
	return append(dst, r.value...)
}

func (r record) KeyEqual(target string) bool {
	return r.key == target
}
