package witness

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/require"
)

var errTest = errors.New("test error")

// requireCaller asserts the contract directly: EventCaller is file:line, and
// that source line contains token — the witness entry point that produced the
// event. Reading the line back from disk keeps the test free of line-number
// offsets, so it survives reformatting and insertions above the call.
func requireCaller(t *testing.T, event Event, token string) {
	t.Helper()

	var i = strings.LastIndexByte(event.EventCaller, ':')
	require.Positive(t, i, "EventCaller %q is not file:line", event.EventCaller)

	var file, number = event.EventCaller[:i], event.EventCaller[i+1:]
	n, err := strconv.Atoi(number)
	require.NoError(t, err, "EventCaller %q is not file:line", event.EventCaller)

	src, err := os.ReadFile(file)
	require.NoError(t, err)
	var lines = strings.Split(string(src), "\n")
	require.LessOrEqual(t, n, len(lines))
	require.Contains(t, lines[n-1], token,
		"event %s: caller %s points at a line that is not a %s call", event.EventType, event.EventCaller, token)
}

func newCallerTest(t *testing.T) (*captureObserver, context.Context) {
	t.Helper()
	var obs = &captureObserver{}
	ctx, _ := Instance(context.Background(), obs, "caller_test", "v1")
	obs.reset()
	return obs, ctx
}

// TestCallers pins the caller contract: every witness entry point attributes
// its event to the line on which that entry point is written. Add a subtest
// here when you add an entry point.
func TestCallers(t *testing.T) {
	t.Run("log helpers", func(t *testing.T) {
		var cases = map[string]func(ctx context.Context){
			"Info(": func(ctx context.Context) {
				Info(ctx, "m")
			},
			"Warn(": func(ctx context.Context) {
				Warn(ctx, "m")
			},
			"Debug(": func(ctx context.Context) {
				Debug(ctx, "m")
			},
			"Error(": func(ctx context.Context) {
				Error(ctx, "m", errTest)
			},
			"ErrorRF(": func(ctx context.Context) {
				_ = ErrorRF(ctx, "m", errTest)
			},
			"ErrorStorage(": func(ctx context.Context) {
				ErrorStorage(ctx, "m", errTest)
			},
			"ErrorStorageF(": func(ctx context.Context) {
				_ = ErrorStorageF(ctx, "m", errTest)
			},
			"ErrorNetwork(": func(ctx context.Context) {
				ErrorNetwork(ctx, "m", errTest)
			},
			"ErrorNetworkF(": func(ctx context.Context) {
				_ = ErrorNetworkF(ctx, "m", errTest)
			},
			"ErrorExternal(": func(ctx context.Context) {
				ErrorExternal(ctx, "m", errTest)
			},
			"ErrorInternal(": func(ctx context.Context) {
				ErrorInternal(ctx, "m", errTest)
			},
			"ErrorOrInfo(": func(ctx context.Context) {
				ErrorOrInfo(ctx, "ok", "err", nil)
			},
			"Observe(": func(ctx context.Context) {
				Observe(ctx, EventTypeLogInfo(), "m")
			},
		}
		for token, call := range cases {
			t.Run(strings.TrimSuffix(token, "("), func(t *testing.T) {
				obs, ctx := newCallerTest(t)
				call(ctx)
				require.Len(t, obs.all(), 1)
				requireCaller(t, obs.last(), token)
			})
		}
	})

	t.Run("Context methods", func(t *testing.T) {
		var cases = map[string]func(c Context){
			".Info(": func(c Context) {
				c.Info("m")
			},
			".Warn(": func(c Context) {
				c.Warn("m")
			},
			".Debug(": func(c Context) {
				c.Debug("m")
			},
			".Error(": func(c Context) {
				c.Error("m", errTest)
			},
		}
		for token, call := range cases {
			t.Run(strings.Trim(token, ".("), func(t *testing.T) {
				obs, ctx := newCallerTest(t)
				call(From(ctx))
				require.Len(t, obs.all(), 1)
				requireCaller(t, obs.last(), token)
			})
		}
	})

	// Span-shaped constructors capture their call site eagerly, so the finish
	// event reports the constructor's position too — not wherever the deferred
	// Finish happened to run.
	t.Run("span constructors", func(t *testing.T) {
		var cases = map[string]func(ctx context.Context) Finish{
			"Span(": func(ctx context.Context) Finish {
				_, f := Span(ctx, "s")
				return f
			},
			"Service(": func(ctx context.Context) Finish {
				_, f := Service(ctx, "s")
				return f
			},
			"Worker(": func(ctx context.Context) Finish {
				_, f := Worker(ctx, "s")
				return f
			},
		}
		for token, open := range cases {
			t.Run(strings.TrimSuffix(token, "("), func(t *testing.T) {
				obs, ctx := newCallerTest(t)
				var finish = open(ctx)
				finish()
				var events = obs.all()
				require.Len(t, events, 2)
				requireCaller(t, events[0], token)
				requireCaller(t, events[1], token)
			})
		}
	})

	t.Run("Instance", func(t *testing.T) {
		var obs = &captureObserver{}
		_, finish := Instance(context.Background(), obs, "i", "v1")
		finish()
		var events = obs.all()
		require.Len(t, events, 2)
		requireCaller(t, events[0], "Instance(")
		requireCaller(t, events[1], "Instance(")
	})

	t.Run("manual spans and messages", func(t *testing.T) {
		var id = uuid.Must(uuid.NewV7())
		var cases = map[string]func(ctx context.Context){
			"SpanStart(": func(ctx context.Context) {
				SpanStart(ctx, id, "s")
			},
			"SpanFinish(": func(ctx context.Context) {
				SpanFinish(ctx, id, "s")
			},
			"InternalMessageSent(": func(ctx context.Context) {
				InternalMessageSent(ctx, "m")
			},
			"InternalMessageReceived(": func(ctx context.Context) {
				InternalMessageReceived(ctx, id, "m")
			},
			"ExternalMessageSent(": func(ctx context.Context) {
				ExternalMessageSent(ctx, "m")
			},
			"Link(": func(ctx context.Context) {
				Link(ctx, "l")
			},
			"LinkTo(": func(ctx context.Context) {
				LinkTo(ctx, id, "l")
			},
			"ExternalMessageReceived(": func(ctx context.Context) {
				ExternalMessageReceived(ctx, id, "m")
			},
		}
		for token, call := range cases {
			t.Run(strings.TrimSuffix(token, "("), func(t *testing.T) {
				obs, ctx := newCallerTest(t)
				call(ctx)
				require.Len(t, obs.all(), 1)
				requireCaller(t, obs.last(), token)
			})
		}
	})

	// The shapes that used to escape the caller's own file entirely, because
	// the old implementation skipped anonymous frames looking for a named one.
	t.Run("closure shapes", func(t *testing.T) {
		t.Run("anonymous func", func(t *testing.T) {
			obs, ctx := newCallerTest(t)
			func() {
				Info(ctx, "m")
			}()
			requireCaller(t, obs.last(), "Info(")
		})

		t.Run("nested closures", func(t *testing.T) {
			obs, ctx := newCallerTest(t)
			func() {
				func() {
					Info(ctx, "m")
				}()
			}()
			requireCaller(t, obs.last(), "Info(")
		})

		t.Run("goroutine literal", func(t *testing.T) {
			obs, ctx := newCallerTest(t)
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				Info(ctx, "m")
			}()
			wg.Wait()
			requireCaller(t, obs.last(), "Info(")
		})

		t.Run("deferred closure", func(t *testing.T) {
			obs, ctx := newCallerTest(t)
			func() {
				defer func() {
					Info(ctx, "m")
				}()
			}()
			requireCaller(t, obs.last(), "Info(")
		})

		t.Run("method", func(t *testing.T) {
			obs, ctx := newCallerTest(t)
			callerTestEmitter{}.emit(ctx)
			requireCaller(t, obs.last(), "Info(")
		})

		t.Run("subtest closure", func(t *testing.T) {
			obs, ctx := newCallerTest(t)
			t.Run("inner", func(t *testing.T) {
				Info(ctx, "m")
			})
			requireCaller(t, obs.last(), "Info(")
		})

		// A house wrapper around witness is attributed to the witness call
		// inside the wrapper, not to whoever called the wrapper. That is the
		// rule holding, not an exception to it: witness.Info really is written
		// on that line. There is no exported way to skip a frame.
		t.Run("user wrapper reports the line inside the wrapper", func(t *testing.T) {
			obs, ctx := newCallerTest(t)
			callerTestWrapper{ctx: ctx}.Info("m")
			requireCaller(t, obs.last(), "Info(w.ctx, msg)")
		})
	})

	// Deferring a witness helper *directly* is unsupported usage, not an
	// exception to the rule: the body runs at function exit and a deferred
	// call's own pc is not reachable from the stack, so the rule cannot be
	// upheld there. Pinned so the behaviour cannot drift silently — write
	// defer func() { witness.Info(...) }() instead (tested above).
	t.Run("deferring a witness helper directly is unsupported", func(t *testing.T) {
		obs, ctx := newCallerTest(t)
		func() {
			defer Info(ctx, "m")
			_ = 0
		}()
		var event = obs.last()
		require.NotEmpty(t, event.EventCaller, "expected a location")

		var i = strings.LastIndexByte(event.EventCaller, ':')
		n, err := strconv.Atoi(event.EventCaller[i+1:])
		require.NoError(t, err)
		src, err := os.ReadFile(event.EventCaller[:i])
		require.NoError(t, err)
		var line = strings.TrimSpace(strings.Split(string(src), "\n")[n-1])
		require.NotContains(t, line, "Info(",
			"a directly deferred witness call is expected to report the exit point; if it now "+
				"reports the defer line, the shape is supported and the docs should say so")
	})
}

type callerTestEmitter struct{}

func (callerTestEmitter) emit(ctx context.Context) { Info(ctx, "m") }

// callerTestWrapper is what a user writes when they wrap witness in their own
// house logger.
type callerTestWrapper struct{ ctx context.Context }

func (w callerTestWrapper) Info(msg string) { Info(w.ctx, msg) }
