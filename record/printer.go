package record

import (
	"encoding/base64"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type PrintFormat uint64

const (
	PrintTime PrintFormat = 1 << iota
	PrintEventID
	PrintCaller
	PrintSpanIDs
	PrintRecords
	PrintLF
)

type Printer struct {
	bufPool               *sync.Pool
	mu                    *sync.Mutex
	maxEventMessageLength int
	maxEventTypeLength    int
	maxEventCallerLength  int
	formatter             Formatter
	printFormat           PrintFormat
}

type PrinterOption func(printer *Printer)

func PrinterWithPrintFormat(value PrintFormat) PrinterOption {
	return func(printer *Printer) {
		printer.printFormat = value
	}
}

func PrinterWithMaxEventTypeLength(length int) PrinterOption {
	return func(printer *Printer) {
		printer.maxEventTypeLength = length
	}
}

func PrinterWithFormatter(formatter Formatter) PrinterOption {
	return func(printer *Printer) {
		printer.formatter = formatter
	}
}

func NewPrinter(options ...PrinterOption) *Printer {
	var bufPool = new(sync.Pool)
	bufPool.New = func() any {
		return make([]byte, 0, 256)
	}
	var p = &Printer{
		bufPool:            bufPool,
		mu:                 new(sync.Mutex),
		maxEventTypeLength: witness.CalcMaxEventValueLength(witness.Events()),
		formatter:          DefaultFormatter{},
		printFormat:        ^PrintFormat(0),
	}
	for _, opt := range options {
		opt(p)
	}
	return p
}

func (p *Printer) appendTime(b []byte, t time.Time) []byte {
	return t.AppendFormat(b, "2006-01-02T15:04:05.000000000Z07:00")
}

func (p *Printer) Append(dst []byte, event witness.Event) []byte {
	p.mu.Lock()
	p.maxEventCallerLength = max(p.maxEventCallerLength, utf8.RuneCountInString(event.EventCaller))
	p.maxEventMessageLength = max(p.maxEventMessageLength, utf8.RuneCountInString(event.EventMessage))
	p.maxEventTypeLength = max(p.maxEventTypeLength, utf8.RuneCountInString(event.EventType.String()))
	var eventCallerSpace = strings.Repeat(" ", p.maxEventCallerLength-utf8.RuneCountInString(event.EventCaller))
	var eventTypeSpace = strings.Repeat(" ", p.maxEventTypeLength-utf8.RuneCountInString(event.EventType.String()))
	var eventMessageSpace = strings.Repeat(" ", p.maxEventMessageLength-utf8.RuneCountInString(event.EventMessage))
	p.mu.Unlock()

	if p.printFormat&PrintTime != 0 {
		dst = p.appendTime(dst, event.EventDate)
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
	if p.printFormat&PrintCaller != 0 {
		dst = append(dst, event.EventCaller...)
		dst = append(dst, eventCallerSpace...)
		dst = append(dst, ' ')
	}
	if p.printFormat&PrintEventID != 0 {
		dst = base64.StdEncoding.AppendEncode(dst, event.EventID.Bytes())
		dst = append(dst, ' ')
	}
	{
		// trace_id is fixed-width (22 chars base64) or 22 spaces when absent —
		// keeps columns aligned so grep / awk pipelines stay simple.
		if event.TraceID != uuid.Nil {
			dst = base64.StdEncoding.AppendEncode(dst, event.TraceID.Bytes())
		} else {
			dst = append(dst, strings.Repeat(" ", 22)...)
		}
		dst = append(dst, ' ')
	}
	if p.printFormat&PrintSpanIDs != 0 {
		dst = append(dst, '[')
		for i, sid := range event.SpanIDs {
			if i != 0 {
				dst = append(dst, ' ')
			}
			dst = base64.StdEncoding.AppendEncode(dst, sid.Bytes())
		}
		dst = append(dst, ']')
	}
	if p.printFormat&PrintRecords != 0 {
		for _, r := range event.Records {
			dst = append(dst, "\n\t"...)
			dst = r.AppendKey(dst)
			dst = append(dst, ": \""...)
			dst = r.AppendValue(dst)
			dst = append(dst, "\""...)
		}
	}
	if p.printFormat&PrintLF != 0 {
		dst = append(dst, '\n')
	}
	return dst
}

func (p *Printer) Print(writer io.Writer, event witness.Event) {
	var buf = p.bufPool.Get().([]byte)
	buf = buf[0:0]
	buf = p.Append(buf, event)
	_, _ = writer.Write(buf)
	p.bufPool.Put(buf)
}
