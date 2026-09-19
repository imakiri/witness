package core

import (
	"runtime"
	"strconv"
	"sync"
)

// callerCache maps a pc to the "file:line" it resolves to. Symbolisation is
// the expensive half of Caller (runtime.CallersFrames plus building the
// string) and its answer never changes for a given pc, while the set of
// distinct pcs is the set of witness call sites in the binary — small,
// fixed, and reached over and over by a hot loop.
var callerCache sync.Map // map[uintptr]string

// SetCallDepth is a no-op.
//
// Deprecated: Caller needs exactly one frame, whatever skip is, so there is
// no depth to configure. It remains so callers that set it still compile.
func SetCallDepth(int) {}

// Caller reports where user code called into witness.
//
// skip counts frames above Caller's own caller: 0 is the line on which Caller
// is written, 1 is that line's caller. Emitting an event directly wants 0;
// a wrapper that emits on someone else's behalf — every entry point in the
// witness package — wants 1, so the event is attributed to the line that
// called the wrapper rather than to the wrapper's own body.
//
// The location is resolved once per call site and cached by pc: the walk
// itself stays (it is what finds the call site) but the symbolisation does
// not repeat, which is what makes a metric in a hot loop affordable.
//
// It is exported because a custom event type is built here in core, so the
// code emitting one calls core.Context.Observe directly and needs a location
// to hand it:
//
//	c := core.From(ctx)
//	c.Observe(myEventType, "cache evicted", core.Caller(0), records...)
//
// The contract, stated in full in CLAUDE.md: the reported location is the line
// on which the witness entry point (Info, Error, Span, SpanFinish, …) is
// written. It is always file:line — there is no alternative format, because a
// function name cannot express the rule.
//
// The line is taken from the *immediate* frame above skip, with no filtering:
// a call made inside a closure, a goroutine literal, or an http.HandlerFunc
// reports the line inside that literal, not the line where the literal was
// created or invoked. A house wrapper around witness is therefore attributed
// to the line inside the wrapper — there is no exported way to skip a frame.
//
// One usage is unsupported because the runtime makes the rule impossible to
// uphold: a witness helper used as a *direct* deferred call. The body of
// `defer witness.Info(...)` runs at function exit, and a deferred call's own pc
// is not reachable from the stack, so the reported line is the exit point.
// Write `defer func() { witness.Info(...) }()` instead — a deferred closure has
// its own frame sitting on the witness call, and reports it correctly.
// Constructors returning a Finish capture their call site eagerly and are immune.
func Caller(skip int) string {
	// One frame is all Caller ever needs: runtime.Callers applies skip
	// itself, so the frame being reported is the first it writes. A
	// fixed-size local array does not escape, which a sync.Pool of slices
	// could not match — Put boxes the slice header into an interface and
	// allocates on every call.
	var pc [1]uintptr

	var n = runtime.Callers(skip+2, pc[:])
	if n == 0 {
		return ""
	}
	if v, ok := callerCache.Load(pc[0]); ok {
		return v.(string)
	}

	var s = resolve(pc[0])
	if s == "" {
		return ""
	}
	callerCache.Store(pc[0], s)
	return s
}

// resolve symbolises one pc. It is a function of its own because
// CallersFrames keeps the slice it is given, which would move Caller's pc
// buffer to the heap on every call — here the escape is paid only on a cache
// miss, i.e. once per call site in the binary.
func resolve(pc uintptr) string {
	var frame, _ = runtime.CallersFrames([]uintptr{pc}).Next()
	if frame.File == "" {
		return ""
	}

	var b = make([]byte, 0, len(frame.File)+8)
	b = append(b, frame.File...)
	b = append(b, ':')
	b = strconv.AppendInt(b, int64(frame.Line), 10)
	return string(b)
}
