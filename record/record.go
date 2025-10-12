package record

import (
	"encoding/base64"
	"fmt"
	"strconv"
)

type Record struct {
	key   string
	value string
}

func (r Record) AppendKey(dst []byte) []byte {
	return append(dst, r.key...)
}

func (r Record) AppendValue(dst []byte) []byte {
	return append(dst, r.value...)
}

func Key(key string) Record {
	return Record{
		key:   key,
		value: "",
	}
}

func String(key string, value string) Record {
	return Record{
		key:   key,
		value: value,
	}
}

func Int(key string, value int) Record {
	return Record{
		key:   key,
		value: strconv.Itoa(value),
	}
}

func Integer(key string, value int64) Record {
	return Record{
		key:   key,
		value: strconv.FormatInt(value, 10),
	}
}

func Number(key string, value uint64) Record {
	return Record{
		key:   key,
		value: strconv.FormatUint(value, 10),
	}
}

func Float(key string, value float64) Record {
	return Record{
		key:   key,
		value: strconv.FormatFloat(value, 'e', -1, 64),
	}
}

type NamedStringer struct {
	key      string
	stringer fmt.Stringer
}

func (r NamedStringer) AppendKey(dst []byte) []byte {
	return append(dst, r.key...)
}

func (r NamedStringer) AppendValue(dst []byte) []byte {
	if r.stringer == nil {
		return dst
	}
	return append(dst, r.stringer.String()...)
}

func Stringer(key string, value fmt.Stringer) NamedStringer {
	return NamedStringer{
		key:      key,
		stringer: value,
	}
}

type ErrorRecord struct {
	key string
	error
}

func (r ErrorRecord) AppendKey(dst []byte) []byte {
	if r.error == nil {
		return dst
	}
	return append(dst, r.key...)
}

func (r ErrorRecord) AppendValue(dst []byte) []byte {
	if r.error == nil {
		return dst
	}
	return append(dst, r.error.Error()...)
}

func Error(key string, err error) ErrorRecord {
	return ErrorRecord{
		key:   key,
		error: err,
	}
}

func Bool(key string, value bool) Record {
	return Record{
		key:   key,
		value: strconv.FormatBool(value),
	}
}

func Bytes(key string, value []byte) Record {
	return Record{
		key:   key,
		value: base64.StdEncoding.EncodeToString(value),
	}
}
