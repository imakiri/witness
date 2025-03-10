package witness

type Record interface {
	Name() string
	String() string
}

type record struct {
	key   string
	value string
}

func (r record) Name() string {
	return r.key
}

func (r record) String() string {
	return r.value
}

//type Source interface {
//	Record() Record
//}
