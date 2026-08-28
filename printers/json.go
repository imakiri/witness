package printers

import (
	"fmt"
	"github.com/go-faster/jx"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
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

func (j *JSON) encode(e *jx.Encoder, event witness.Event, flags witness.PrintFlags) {
	e.Obj(func(e *jx.Encoder) {
		if event.TraceID != uuid.Nil {
			e.Field("trace_id", func(e *jx.Encoder) {
				e.Base64(event.TraceID.Bytes())
			})
		}
		if flags&witness.PrintEventID != witness.PrintNone {
			e.Field("event_id", func(e *jx.Encoder) {
				e.Base64(event.EventID.Bytes())
			})
		}
		if flags&witness.PrintTime != witness.PrintNone {
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
		if flags&witness.PrintCaller != witness.PrintNone {
			e.Field("event_caller", func(e *jx.Encoder) {
				e.Str(event.EventCaller)
			})
		}
		if flags&witness.PrintSpanIDs != witness.PrintNone {
			e.Field("event_span_ids", func(e *jx.Encoder) {
				e.Arr(func(e *jx.Encoder) {
					for _, sid := range event.SpanIDs {
						e.Base64(sid.Bytes())
					}
				})
			})
		}
		if flags&witness.PrintRecords != witness.PrintNone {
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
	if flags&witness.PrintCR != witness.PrintNone {
		b = append(b, '\r')
	}
	if flags&witness.PrintLF != witness.PrintNone {
		b = append(b, '\n')
	}
	e.SetBytes(b)
}

func (j *JSON) Append(dst []byte, event witness.Event, flags witness.PrintFlags) []byte {
	e := jx.GetEncoder()
	j.encode(e, event, flags)
	dst = append(dst, e.Bytes()...)
	jx.PutEncoder(e)
	return dst
}

func (j *JSON) Print(writer io.Writer, event witness.Event, flags witness.PrintFlags) {
	e := jx.GetEncoder()
	j.encode(e, event, flags)
	_, _ = writer.Write(e.Bytes())
	jx.PutEncoder(e)
}
