package record

import (
	"fmt"
	"github.com/imakiri/witness"
	"reflect"
)

type Formatter interface {
	Structure(path, key string) string
	Map(path string, key reflect.Value) string
	Array(path string, key int) string
	Slice(path string, key int) string
}

type Marshaller[F Formatter] struct {
	MaxDepth     uint64
	KeyFormatter F
}

func (m Marshaller[F]) Marshal(key string, value any, prefix ...witness.Record) []witness.Record {
	return append(prefix, m.marshal(key, 0, reflect.ValueOf(value), nil)...)
}

func (m Marshaller[F]) marshal(key string, depth uint64, v reflect.Value, records []witness.Record) []witness.Record {
	if depth >= m.MaxDepth {
		return records
	} else {
		depth++
	}

	if !v.IsValid() {
		return append(records, Stringer(key, v))
	}

	if v.Type().Implements(reflect.TypeFor[fmt.Stringer]()) {
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
			var fieldKey = m.KeyFormatter.Structure(key, v.Type().Field(i).Name)
			records = m.marshal(fieldKey, depth, v.Field(i), records)
		}
		return records
	case reflect.Map:
		var iter = v.MapRange()
		for iter.Next() {
			records = m.marshal(m.KeyFormatter.Map(key, iter.Key()), depth, iter.Value(), records)
		}
		return records
	case reflect.Array:
		if reflect.TypeOf([]byte(nil)) == v.Type() {
			return append(records, Bytes(key, v.Bytes()))
		}
		for i := 0; i < v.Len(); i++ {
			records = m.marshal(m.KeyFormatter.Array(key, i), depth, v.Index(i), records)
		}
		return records
	case reflect.Slice:
		if reflect.TypeOf([]byte(nil)) == v.Type() {
			return append(records, Bytes(key, v.Bytes()))
		}
		for i := 0; i < v.Len(); i++ {
			records = m.marshal(m.KeyFormatter.Slice(key, i), depth, v.Index(i), records)
		}
		return records
	default:
		return records
	}
}
