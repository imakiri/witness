package record

// Map applies f to every element of s and returns the results, in order.
//
// It is here rather than in witness because it is not observability: the
// call that wants it is usually witness.HandleAll, which takes the msgIDs of
// a batch, and pulling one field out of a slice of jobs is a loop the caller
// would otherwise write by hand.
func Map[T, R any](s []T, f func(T) R) []R {
	r := make([]R, len(s))
	for i, v := range s {
		r[i] = f(v)
	}
	return r
}
