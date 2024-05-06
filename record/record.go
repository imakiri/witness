package record

import (
	"encoding/base64"
	"fmt"
	"reflect"
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
	panic("should not be called, Rob Pike should")
}

func (r Records) String() string {
	panic("should not be called, Rob Pike should")
}

func Any(key string, value any) Records {
	return parse(key, reflect.ValueOf(value), nil)
}

func parse(key string, v reflect.Value, records Records) Records {
	switch v.Kind() {
	case reflect.Pointer:
		return append(records, parse(key, v.Elem(), records)...)
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
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			records = parse(fmt.Sprintf("%s.%s", key, v.Type().Field(i).Name), v.Field(i), records)
		}
		return records
	case reflect.Map:
		var iter = v.MapRange()
		for iter.Next() {
			records = parse(fmt.Sprintf("%s.%s", key, iter.Key()), iter.Value(), records)
		}
		return records
	case reflect.Array, reflect.Slice:
		if reflect.TypeOf([]byte(nil)) == v.Type() {
			return append(records, Bytes(key, v.Bytes()))
		}
		for i := 0; i < v.Len(); i++ {
			records = parse(fmt.Sprintf("%s[%d]", key, i), v.Index(i), records)
		}
		return records
	default:
		return records
	}
}
