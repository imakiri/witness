package record

import (
	"fmt"
	"strconv"
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
