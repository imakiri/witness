package record

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

type Record struct {
	name  string
	value string
}

func (r Record) Name() string {
	return r.name
}

func (r Record) String() string {
	return r.value
}

func Name(name string) Record {
	return Record{
		name:  name,
		value: "",
	}
}

func String(name string, value string) Record {
	return Record{
		name:  name,
		value: value,
	}
}

func Int(name string, value int) Record {
	return Record{
		name:  name,
		value: strconv.Itoa(value),
	}
}

func Integer(name string, value int64) Record {
	return Record{
		name:  name,
		value: strconv.FormatInt(value, 10),
	}
}

func Number(name string, value uint64) Record {
	return Record{
		name:  name,
		value: strconv.FormatUint(value, 10),
	}
}

func Float(name string, value float64) Record {
	return Record{
		name:  name,
		value: strconv.FormatFloat(value, 'e', -1, 64),
	}
}

type NamedStringer struct {
	name string
	fmt.Stringer
}

func (r NamedStringer) Name() string {
	return r.name
}

func Stringer(name string, value fmt.Stringer) NamedStringer {
	return NamedStringer{
		name:     name,
		Stringer: value,
	}
}

type ErrorRecord struct {
	name string
	error
}

func (r ErrorRecord) Name() string {
	return r.name
}

func (r ErrorRecord) String() string {
	if r.error != nil {
		return r.Error()
	}
	return "nil"
}

func Error(name string, err error) ErrorRecord {
	return ErrorRecord{
		name:  name,
		error: err,
	}
}

func Bool(name string, value bool) Record {
	return Record{
		name:  name,
		value: strconv.FormatBool(value),
	}
}

func Bytes(name string, value []byte) Record {
	return Record{
		name:  name,
		value: base64.StdEncoding.EncodeToString(value),
	}
}

type Records []Record

func (r Records) Name() string {
	var s strings.Builder
	for i := range r {
		s.WriteString(r[i].Name())
		s.WriteRune(',')
		s.WriteRune(' ')
	}
	return s.String()[:s.Len()-2]
}

func (r Records) String() string {
	var s strings.Builder
	for i := range r {
		s.WriteString(r[i].Name())
		s.WriteRune(':')
		s.WriteString(r[i].String())
		s.WriteRune('\n')
	}
	return s.String()[:s.Len()-1]
}
