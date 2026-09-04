package record

import (
	"fmt"
	"github.com/imakiri/witness/core"
	"reflect"
)

// Formatter
//
// Deprecated: use KeyJoiner
type Formatter = KeyJoiner

type KeyJoiner interface {
	Structure(path, key string) string
	Map(path string, key reflect.Value) string
	Array(path string, key int) string
	Slice(path string, key int) string
}

type Marshaller[KJ KeyJoiner] struct {
	MaxDepth       uint64
	KeyJoiner      KJ
	PreferStringer bool
}

func (m Marshaller[KJ]) Marshal(key string, value any, prefix ...core.Record) []core.Record {
	return append(prefix, m.marshal(key, 0, reflect.ValueOf(value), nil)...)
}

func (m Marshaller[KJ]) marshal(key string, depth uint64, v reflect.Value, records []core.Record) []core.Record {
	if depth >= m.MaxDepth {
		return records
	} else {
		depth++
	}

	if !v.IsValid() {
		return append(records, Stringer(key, v))
	}

	// CanInterface is false for values read out of unexported struct fields;
	// v.Interface() would panic on them.
	if m.PreferStringer && v.CanInterface() && v.Type().Implements(reflect.TypeFor[fmt.Stringer]()) {
		return append(records, Stringer(key, (v.Interface()).(fmt.Stringer)))
	}

	switch v.Kind() {
	case reflect.String:
		return append(records, String(key, v.String()))
	case reflect.Int, reflect.Int64, reflect.Int8, reflect.Int16, reflect.Int32:
		return append(records, Integer(key, v.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return append(records, Number(key, v.Uint()))
	case reflect.Bool:
		return append(records, Bool(key, v.Bool()))
	case reflect.Float32, reflect.Float64:
		return append(records, Float(key, v.Float()))
	case reflect.Pointer, reflect.Interface:
		return m.marshal(key, depth, v.Elem(), records)
	case reflect.Struct:
		if v.NumField() == 0 {
			return append(records, String(key, "{}"))
		}
		for i := 0; i < v.NumField(); i++ {
			var fieldKey = m.KeyJoiner.Structure(key, v.Type().Field(i).Name)
			records = m.marshal(fieldKey, depth, v.Field(i), records)
		}
		return records
	case reflect.Map:
		var iter = v.MapRange()
		for iter.Next() {
			records = m.marshal(m.KeyJoiner.Map(key, iter.Key()), depth, iter.Value(), records)
		}
		return records
	case reflect.Array:
		if b, ok := byteSequence(v); ok {
			return append(records, Bytes(key, b))
		}
		for i := 0; i < v.Len(); i++ {
			records = m.marshal(m.KeyJoiner.Array(key, i), depth, v.Index(i), records)
		}
		return records
	case reflect.Slice:
		if b, ok := byteSequence(v); ok {
			return append(records, Bytes(key, b))
		}
		for i := 0; i < v.Len(); i++ {
			records = m.marshal(m.KeyJoiner.Slice(key, i), depth, v.Index(i), records)
		}
		return records
	default:
		return records
	}
}

// byteSequence reports whether v is a slice or array of bytes and returns its
// contents, so that []byte renders as one base64 record instead of one record
// per element.
//
// The test is on the element's *kind*, not on the type: comparing v.Type()
// against []byte missed every named type (`type Payload []byte`), and on the
// array branch it could never be true at all — an array type is never equal
// to a slice type, so that branch was dead.
//
// Arrays are copied out element by element. A value reached through
// reflect.ValueOf is not addressable, and v.Bytes() panics on an
// unaddressable byte array.
func byteSequence(v reflect.Value) ([]byte, bool) {
	if v.Type().Elem().Kind() != reflect.Uint8 {
		return nil, false
	}
	if v.Kind() == reflect.Slice {
		return v.Bytes(), true
	}
	var b = make([]byte, v.Len())
	for i := range b {
		b[i] = byte(v.Index(i).Uint())
	}
	return b, true
}
