package core

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var (
	callDepth int
	pcPool    *sync.Pool
)

func SetCallDepth(i int) {
	callDepth = i
	pcPool = new(sync.Pool)
	pcPool.New = func() any {
		return make([]uintptr, callDepth)
	}
}

func init() {
	SetCallDepth(16)
}

// Caller reports where user code called into witness.
//
// skip counts frames above Caller's own caller: 0 is the line on which Caller
// is written, 1 is that line's caller. Emitting an event directly wants 0;
// a wrapper that emits on someone else's behalf — every entry point in the
// witness package — wants 1, so the event is attributed to the line that
// called the wrapper rather than to the wrapper's own body.
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
	var pc = pcPool.Get().([]uintptr)
	defer pcPool.Put(pc)

	var n = runtime.Callers(skip+2, pc)
	if n == 0 {
		return ""
	}

	// CallersFrames does the return-address adjustment and expands inlined
	// frames; FuncForPC/FileLine on the raw pc does neither, which is why
	// line numbers used to drift by a build-flags-dependent amount.
	var frame, _ = runtime.CallersFrames(pc[:n]).Next()
	if frame.File == "" {
		return ""
	}

	var c strings.Builder
	c.WriteString(frame.File)
	c.WriteRune(':')
	c.WriteString(strconv.Itoa(frame.Line))
	return c.String()
}
