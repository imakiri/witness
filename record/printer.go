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

type Printer struct {
	bufPool               *sync.Pool
	mu                    *sync.Mutex
	maxEventMessageLength int
	maxEventTypeLength    int
	maxEventCallerLength  int
	formatter             Formatter
	printCaller           bool
}

type PrinterOption func(printer *Printer)

func PrinterWithPrintCaller(value bool) PrinterOption {
	return func(printer *Printer) {
		printer.printCaller = value
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
		printCaller:        true,
	}
	for _, opt := range options {
		opt(p)
	}
	return p
}

func (p *Printer) appendTime(b []byte, t time.Time) []byte {
	return t.AppendFormat(b, "2006-01-02T15:04:05.000000000Z07:00")
}

func (p *Printer) Append(dst []byte, spanIDs []uuid.UUID, eventID uuid.UUID, eventDate time.Time, eventType witness.EventType, eventMessage string, eventCaller string, records ...witness.Record) []byte {
	p.mu.Lock()
	p.maxEventCallerLength = max(p.maxEventCallerLength, utf8.RuneCountInString(eventCaller))
	p.maxEventMessageLength = max(p.maxEventMessageLength, utf8.RuneCountInString(eventMessage))
	p.maxEventTypeLength = max(p.maxEventTypeLength, utf8.RuneCountInString(eventType.String()))
	var eventCallerSpace = strings.Repeat(" ", p.maxEventCallerLength-utf8.RuneCountInString(eventCaller))
	var eventTypeSpace = strings.Repeat(" ", p.maxEventTypeLength-utf8.RuneCountInString(eventType.String()))
	var eventMessageSpace = strings.Repeat(" ", p.maxEventMessageLength-utf8.RuneCountInString(eventMessage))
	p.mu.Unlock()

	dst = append(dst, '\n')
	dst = p.appendTime(dst, eventDate)
	dst = append(dst, ' ')
	dst = base64.StdEncoding.AppendEncode(dst, eventID.Bytes())
	dst = append(dst, ' ')
	if p.printCaller {
		dst = append(dst, eventCaller...)
		dst = append(dst, eventCallerSpace...)
		dst = append(dst, ' ')
	}
	dst = eventType.Append(dst)
	dst = append(dst, eventTypeSpace...)
	dst = append(dst, ' ')
	dst = append(dst, eventMessage...)
	dst = append(dst, eventMessageSpace...)
	dst = append(dst, ' ')
	dst = append(dst, '[')
	for i, sid := range spanIDs {
		if i != 0 {
			dst = append(dst, ' ')
		}
		dst = base64.StdEncoding.AppendEncode(dst, sid.Bytes())
	}
	dst = append(dst, ']')
	for _, r := range records {
		dst = append(dst, "\n\t"...)
		dst = r.AppendKey(dst)
		dst = append(dst, ": \""...)
		dst = r.AppendValue(dst)
		dst = append(dst, "\""...)
	}
	return dst
}

func (p *Printer) Print(writer io.Writer, spanIDs []uuid.UUID, eventID uuid.UUID, eventDate time.Time, eventType witness.EventType, eventMessage string, eventCaller string, records ...witness.Record) {
	var buf = p.bufPool.Get().([]byte)
	buf = buf[0:0]
	buf = p.Append(buf, spanIDs, eventID, eventDate, eventType, eventMessage, eventCaller, records...)
	_, _ = writer.Write(buf)
	p.bufPool.Put(buf)
}
