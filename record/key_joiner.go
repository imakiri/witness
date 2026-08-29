package record

import (
	"fmt"
	"reflect"
)

// DefaultFormatter
//
// Deprecated: use DefaultKeyJoiner
type DefaultFormatter = DefaultKeyJoiner

type DefaultKeyJoiner struct{}

func (d DefaultKeyJoiner) Structure(path, key string) string {
	if path == "" {
		return key
	}
	return fmt.Sprintf("%s.%s", path, key)
}

func (d DefaultKeyJoiner) Map(path string, key reflect.Value) string {
	if path == "" {
		return fmt.Sprintf("[%#v]", key)
	}
	return fmt.Sprintf("%s[%#v]", path, key)
}

func (d DefaultKeyJoiner) Array(path string, key int) string {
	if path == "" {
		return fmt.Sprintf("[%d]", key)
	}
	return fmt.Sprintf("%s[%d]", path, key)
}

func (d DefaultKeyJoiner) Slice(path string, key int) string {
	if path == "" {
		return fmt.Sprintf("[%d]", key)
	}
	return fmt.Sprintf("%s[%d]", path, key)
}
