package printers

import (
	"fmt"
	"github.com/go-faster/jx"
	"github.com/imakiri/witness/core"
	"io"
	"time"
)

type JSON struct {
}

type JSONOption func(printer *JSON) error

func NewJSON(options ...JSONOption) (*JSON, error) {
	var p = &JSON{}
	for i, option := range options {
		if err := option(p); err != nil {
			return nil, fmt.Errorf("failed at option %d: %w", i, err)
		}
	}
	return p, nil
}

func (j *JSON) encode(e *jx.Encoder, event core.Event, flags core.PrintFlags) {
	e.Obj(func(e *jx.Encoder) {
		if flags&core.PrintEventID != core.PrintNone {
			e.Field("event_id", func(e *jx.Encoder) {
				e.Base64(event.EventID.Bytes())
			})
		}
		if flags&core.PrintTime != core.PrintNone {
			e.Field("event_date", func(e *jx.Encoder) {
				e.ByteStr(event.EventDate.AppendFormat(make([]byte, 0, 36), time.RFC3339Nano))
			})
		}
		e.Field("event_type", func(e *jx.Encoder) {
			e.ByteStr(event.EventType.Append(nil))
		})
		e.Field("event_message", func(e *jx.Encoder) {
			e.Str(event.EventMessage)
		})
		if flags&core.PrintCaller != core.PrintNone {
			e.Field("event_caller", func(e *jx.Encoder) {
				e.Str(event.EventCaller)
			})
		}
		if flags&core.PrintSpanIDs != core.PrintNone {
			e.Field("event_span_ids", func(e *jx.Encoder) {
				e.Arr(func(e *jx.Encoder) {
					for _, sid := range event.SpanIDs {
						e.Base64(sid.Bytes())
					}
				})
			})
		}
		if flags&core.PrintRecords != core.PrintNone {
			e.Field("event_records", func(e *jx.Encoder) {
				e.Arr(func(e *jx.Encoder) {
					for _, r := range event.Records {
						e.Obj(func(e *jx.Encoder) {
							e.Field("key", func(e *jx.Encoder) {
								e.ByteStr(r.AppendKey(nil))
							})
							e.Field("value", func(e *jx.Encoder) {
								e.ByteStr(r.AppendValue(nil))
							})
						})
					}
				})
			})
		}
	})

	b := e.Bytes()
	if flags&core.PrintCR != core.PrintNone {
		b = append(b, '\r')
	}
	if flags&core.PrintLF != core.PrintNone {
		b = append(b, '\n')
	}
	e.SetBytes(b)
}

func (j *JSON) Append(dst []byte, event core.Event, flags core.PrintFlags) []byte {
	e := jx.GetEncoder()
	j.encode(e, event, flags)
	dst = append(dst, e.Bytes()...)
	jx.PutEncoder(e)
	return dst
}

func (j *JSON) Print(writer io.Writer, event core.Event, flags core.PrintFlags) {
	e := jx.GetEncoder()
	j.encode(e, event, flags)
	_, _ = writer.Write(e.Bytes())
	jx.PutEncoder(e)
}
