package printers

import (
	"encoding/base64"
	"fmt"
	"github.com/imakiri/witness/core"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

type Pretty struct {
	bufPool               *sync.Pool
	mu                    *sync.Mutex
	maxEventMessageLength int
	maxEventTypeLength    int
	maxEventCallerLength  int
}

type PrettyOption func(printer *Pretty) error

func NewPretty(options ...PrettyOption) (*Pretty, error) {
	var bufPool = new(sync.Pool)
	bufPool.New = func() any {
		return make([]byte, 0, 256)
	}
	var p = &Pretty{
		bufPool:            bufPool,
		mu:                 new(sync.Mutex),
		maxEventTypeLength: core.CalcMaxEventValueLength(core.Events()),
	}
	for i, option := range options {
		if err := option(p); err != nil {
			return nil, fmt.Errorf("failed at option %d: %w", i, err)
		}
	}
	return p, nil
}

func (p *Pretty) Append(dst []byte, event core.Event, flags core.PrintFlags) []byte {
	p.mu.Lock()
	p.maxEventCallerLength = max(p.maxEventCallerLength, utf8.RuneCountInString(event.EventCaller))
	p.maxEventMessageLength = max(p.maxEventMessageLength, utf8.RuneCountInString(event.EventMessage))
	p.maxEventTypeLength = max(p.maxEventTypeLength, utf8.RuneCountInString(event.EventType.String()))
	var eventCallerSpace = strings.Repeat(" ", p.maxEventCallerLength-utf8.RuneCountInString(event.EventCaller))
	var eventTypeSpace = strings.Repeat(" ", p.maxEventTypeLength-utf8.RuneCountInString(event.EventType.String()))
	var eventMessageSpace = strings.Repeat(" ", p.maxEventMessageLength-utf8.RuneCountInString(event.EventMessage))
	p.mu.Unlock()

	if flags&core.PrintTime != core.PrintNone {
		dst = event.EventDate.AppendFormat(dst, "2006-01-02T15:04:05.000000000Z07:00")
		dst = append(dst, ' ')
	}
	{
		dst = event.EventType.Append(dst)
		dst = append(dst, eventTypeSpace...)
		dst = append(dst, ' ')
	}
	{
		dst = append(dst, event.EventMessage...)
		dst = append(dst, eventMessageSpace...)
		dst = append(dst, ' ')
	}
	if flags&core.PrintCaller != core.PrintNone {
		dst = append(dst, event.EventCaller...)
		dst = append(dst, eventCallerSpace...)
		dst = append(dst, ' ')
	}
	if flags&core.PrintEventID != core.PrintNone {
		dst = base64.StdEncoding.AppendEncode(dst, event.EventID.Bytes())
		dst = append(dst, ' ')
	}
	if flags&core.PrintSpanIDs != core.PrintNone {
		dst = append(dst, '[')
		for i, sid := range event.SpanIDs {
			if i != 0 {
				dst = append(dst, ' ')
			}
			dst = base64.StdEncoding.AppendEncode(dst, sid.Bytes())
		}
		dst = append(dst, ']')
	}
	if flags&core.PrintRecords != core.PrintNone {
		for _, r := range event.Records {
			dst = append(dst, "\n\t"...)
			dst = r.AppendKey(dst)
			dst = append(dst, ": \""...)
			dst = r.AppendValue(dst)
			dst = append(dst, "\""...)
		}
	}
	if flags&core.PrintCR != core.PrintNone {
		dst = append(dst, '\r')
	}
	if flags&core.PrintLF != core.PrintNone {
		dst = append(dst, '\n')
	}
	return dst
}

func (p *Pretty) Print(writer io.Writer, event core.Event, flags core.PrintFlags) {
	var buf = p.bufPool.Get().([]byte)
	buf = buf[0:0]
	buf = p.Append(buf, event, flags)
	_, _ = writer.Write(buf)
	clear(buf)
	p.bufPool.Put(buf)
}
