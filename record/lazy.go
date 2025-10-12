package record

//import "github.com/imakiri/witness"
//
//type lazyRecord struct {
//	f func() witness.Record
//	c witness.Record
//}
//
//func (l *lazyRecord) AppendKey() string {
//	if l == nil {
//		return ""
//	}
//	if l.c == nil {
//		l.c = l.f()
//	}
//	return l.c.AppendKey()
//}
//
//func (l *lazyRecord) AppendValue() string {
//	if l == nil {
//		return ""
//	}
//	if l.c == nil {
//		l.c = l.f()
//	}
//	return l.c.AppendValue()
//}
//
//func Lazy(f func() witness.Record) witness.Record {
//	if f == nil {
//		return nil
//	}
//	return &lazyRecord{
//		f: f,
//	}
//}
