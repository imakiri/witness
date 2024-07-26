package record

import (
	"fmt"
	"reflect"
)

type DefaultFormatter struct{}

func (d DefaultFormatter) Structure(path, key string) string {
	if path == "" {
		return key
	}
	return fmt.Sprintf("%s.%s", path, key)
}

func (d DefaultFormatter) Map(path string, key reflect.Value) string {
	if path == "" {
		return fmt.Sprintf("[%#v]", key)
	}
	return fmt.Sprintf("%s[%#v]", path, key)
}

func (d DefaultFormatter) Array(path string, key int) string {
	if path == "" {
		return fmt.Sprintf("[%d]", key)
	}
	return fmt.Sprintf("%s[%d]", path, key)
}

func (d DefaultFormatter) Slice(path string, key int) string {
	if path == "" {
		return fmt.Sprintf("[%d]", key)
	}
	return fmt.Sprintf("%s[%d]", path, key)
}
