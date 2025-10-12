package witness

type Record interface {
	AppendKey(dst []byte) []byte
	AppendValue(dst []byte) []byte
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
