package record

import "github.com/imakiri/witness"

type lazyRecord struct {
	f func() witness.Record
	c witness.Record
}

func (l *lazyRecord) Name() string {
	if l == nil {
		return ""
	}
	if l.c == nil {
		l.c = l.f()
	}
	return l.c.Name()
}

func (l *lazyRecord) String() string {
	if l == nil {
		return ""
	}
	if l.c == nil {
		l.c = l.f()
	}
	return l.c.String()
}

func Lazy(f func() witness.Record) witness.Record {
	if f == nil {
		return nil
	}
	return &lazyRecord{
		f: f,
	}
}

//func LazySource(s witness.Source) witness.Record {
//	if s == nil {
//		return nil
//	}
//	return &lazyRecord{
//		f: s.Record,
//	}
//}
