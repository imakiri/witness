package queries

import (
	"regexp"
	"strconv"
)

// traceWalkCTE resolves a trace: the set of spans reachable from one or more
// root spans, tagged with the root each was reached from. It expects the
// roots as $1::uuid[].
//
// Witness has no trace_id — a trace is a connected component of the
// event<->span graph — so the component is walked at query time. The walk is
// *directed*: parent -> child, and sender -> receiver across a link. It is
// deliberately not plain co-occurrence in witness.spans, because every event
// of a process carries that process's instance span, so co-occurrence would
// merge the entire process lifetime into one component.
//
// Both edge kinds come from views built on span_flags, so a cross-process hop
// needs no special case: the shared span_id is an ordinary link row.
const traceWalkCTE = `
WITH RECURSIVE
  trace_edges AS (
      SELECT parent_span_id AS from_id, child_span_id AS to_id
        FROM witness.span_children
    UNION ALL
      SELECT from_span_id, to_span_id
        FROM witness.link_edges
  ),
  trace_spans AS (
      SELECT r.root AS root, r.root AS span_id
        FROM unnest($1::uuid[]) AS r(root)
    UNION
      SELECT ts.root, te.to_id
        FROM trace_edges te
        JOIN trace_spans ts ON ts.span_id = te.from_id
  )
`

// errorEventTypes is the set of event_type values whose EventType.IsError()
// is true in witness/events.go. Kept in one place so the panels agree.
const errorEventTypes = "13, 14, 100, 101, 102, 103, 104"

// logEventTypes is log and log:* — what the trace view attaches to a span.
const logEventTypes = "1, 10, 11, 12, 13, 14, 100, 101, 102, 103, 104"

// shiftPlaceholders renumbers $N -> $N+1 in a WHERE fragment. Needed when a
// query gains the trace-walk CTE, whose seed must be $1.
func shiftPlaceholders(clause string) string {
	return placeholderRe.ReplaceAllStringFunc(clause, func(m string) string {
		n, err := strconv.Atoi(m[1:])
		if err != nil {
			return m
		}
		return "$" + strconv.Itoa(n+1)
	})
}

var placeholderRe = regexp.MustCompile(`\$\d+`)
