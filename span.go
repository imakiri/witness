package witness

import "unicode/utf8"

type SpanType struct {
	s string
	i int64
}

func MustNewSpanType(i int64, s string) SpanType {
	if i < 1000 {
		panic("i values below 1000 are reserved")
	}
	if utf8.RuneCountInString(s) > 127 {
		panic("s values cannot exceed 128 characters")
	}
	return SpanType{
		i: i,
		s: s,
	}
}

func (st SpanType) String() string {
	return st.s
}

func (st SpanType) Integer() int64 {
	return st.i
}

func SpanTypeFunction() SpanType {
	return SpanType{
		s: "function",
		i: 1,
	}
}
