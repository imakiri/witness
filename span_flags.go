package witness

// SpanFlags is the role a span_id plays in one event's span chain. An event
// carries N span_ids and each of them relates to the event differently: one
// is the span the event happened *in*, the others are enclosing scopes, the
// emitting process, or a span borrowed from somewhere else. Without the
// roles the chain is a bag — `span_id = X` cannot tell "in X" from "under
// X", and parent/child has to be guessed from timestamps.
//
// The flags are a bitmask because the roles combine: the root event of an
// instance is SpanFlagOwn|SpanFlagInstance, and a message hand-off's msgID
// is SpanFlagOwn|SpanFlagLink.
//
// Width is int64 to match the `span_flags int8` column in Postgres and to
// leave room; the five roles below fit in a byte.
type SpanFlags int64

const (
	// SpanFlagNone is the empty mask: an ordinary locally-minted span with
	// no durable role of its own.
	SpanFlagNone SpanFlags = 0

	// SpanFlagOwn marks the span the event happened directly in — the
	// deepest span of the chain. Exactly one per event. It does not say
	// whether the event is that span's boundary or merely located inside
	// it: EventType already answers that (span:*:start / span:*:finish
	// versus everything else).
	SpanFlagOwn SpanFlags = 1 << 0

	// SpanFlagParent marks the direct parent of the own span. Absent when
	// the chain has a single element. Together with SpanFlagOwn on a span's
	// start event this reconstructs the span tree exactly, with no
	// timestamp heuristics.
	SpanFlagParent SpanFlags = 1 << 1

	// SpanFlagAncestor marks an enclosing scope above the parent.
	SpanFlagAncestor SpanFlags = 1 << 2

	// SpanFlagInstance marks the root span of the process that emitted the
	// event — the span minted by Instance or Test, the only constructors
	// there are, and always the chain's first span since nothing sits above
	// an instance. Exactly one per event, and the emitter is
	// always a single process, so this is the span-chain answer to "who
	// wrote this".
	SpanFlagInstance SpanFlags = 1 << 3

	// SpanFlagLink marks a span this event merely *references*: the shared
	// point of a hand-off (Link / LinkTo, the message helpers). It carries
	// no positional role — a link is never own, parent or ancestor, because
	// the process never enters it. It sits after the chain in
	// Event.SpanIDs.
	//
	// A link is the one place where span_id -> instance stops being a
	// function: both sides of a hand-off emit events referencing it, which
	// is exactly how a query on that span_id reconnects them. Neither side
	// opens or closes it as if it were their own scope — that would put two
	// processes' start/finish events on one span and make its reconstructed
	// duration meaningless.
	SpanFlagLink SpanFlags = 1 << 4
)

// positionalSpanFlag is the role index i plays in a chain of n spans. The
// chain is ordered root -> leaf, so position alone decides every role, and
// they are recomputed for every event because they change as the chain
// grows: today's own span is tomorrow's parent.
func positionalSpanFlag(i, n int) SpanFlags {
	var f SpanFlags
	switch {
	case i == n-1:
		f = SpanFlagOwn
	case i == n-2:
		f = SpanFlagParent
	default:
		f = SpanFlagAncestor
	}
	if i == 0 {
		// Nothing sits above an instance and Instance / Test are the only
		// constructors, so the chain's first span is always the process
		// root. Nothing about it needs storing.
		f |= SpanFlagInstance
	}
	return f
}

// eventSpanFlags is the per-span mask for an event emitted from this
// Context with nLinks referenced spans appended after the chain. Chain
// entries get their positional role; link entries get SpanFlagLink and
// nothing else, because the process never entered them.
func (c Context) eventSpanFlags(nLinks int) []SpanFlags {
	var n = len(c.spanIDs)
	if n == 0 {
		return nil
	}
	var fs = make([]SpanFlags, n+nLinks)
	for i := range n {
		fs[i] = positionalSpanFlag(i, n)
	}
	for i := n; i < n+nLinks; i++ {
		fs[i] = SpanFlagLink
	}
	return fs
}
