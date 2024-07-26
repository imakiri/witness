package record

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

type Record struct {
	key   string
	value string
}

func (r Record) Key() string {
	return r.key
}

func (r Record) String() string {
	return r.value
}

func New(key string, value string) Record {
	return Record{
		key:   key,
		value: value,
	}
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
	key string
	fmt.Stringer
}

func (r NamedStringer) Key() string {
	return r.key
}

func Stringer(key string, value fmt.Stringer) NamedStringer {
	return NamedStringer{
		key:      key,
		Stringer: value,
	}
}

type ErrorRecord struct {
	key string
	error
}

func (r ErrorRecord) Key() string {
	return r.key
}

func (r ErrorRecord) String() string {
	if r.error != nil {
		return r.Error()
	}
	return "nil"
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

type Records []Record

func (r Records) Key() string {
	var s strings.Builder
	for i := range r {
		s.WriteString(r[i].Key())
		s.WriteRune(',')
		s.WriteRune(' ')
	}
	return s.String()[:s.Len()-2]
}

func (r Records) String() string {
	var s strings.Builder
	for i := range r {
		s.WriteString(r[i].Key())
		s.WriteRune(':')
		s.WriteString(r[i].String())
		s.WriteRune('\n')
	}
	return s.String()[:s.Len()-1]
}
