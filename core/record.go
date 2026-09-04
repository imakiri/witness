package core

// Record is one key/value pair attached to an event. The value is rendered
// through AppendValue into a caller-provided buffer rather than returned as a
// string, so an implementation can stream into it without allocating.
type Record interface {
	AppendKey(dst []byte) []byte
	AppendValue(dst []byte) []byte
	KeyEqual(target string) bool
}
