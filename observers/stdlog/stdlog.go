package stdlog

import (
	"encoding/base64"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Observer struct {
	bufPool               *sync.Pool
	mu                    *sync.Mutex
	maxEventMessageLength int
	maxEventValueLength   int
	maxEventCallerLength  int
	formatter             record.Formatter
	printCaller           bool
}

type Option func(o *Observer)

func WithPrintCaller(value bool) Option {
	return func(o *Observer) {
		o.printCaller = value
	}
}

func NewObserver(options ...Option) *Observer {
	var bufPool = new(sync.Pool)
	bufPool.New = func() any {
		return make([]byte, 0, 256)
	}
	var o = &Observer{
		bufPool: bufPool,
		mu:      new(sync.Mutex),
		//maxEventMessageLength: 8,
		maxEventValueLength: 8,
		formatter:           record.DefaultFormatter{},
		printCaller:         true,
	}
	for _, opt := range options {
		opt(o)
	}
	return o
}

func (o *Observer) appendTime(b []byte, t time.Time) []byte {
	return t.AppendFormat(b, "2006-01-02T15:04:05.000000000Z07:00")
}

func (o *Observer) Observe(event witness.Event) {
	o.mu.Lock()
	o.maxEventCallerLength = max(o.maxEventCallerLength, utf8.RuneCountInString(event.EventCaller))
	o.maxEventMessageLength = max(o.maxEventMessageLength, utf8.RuneCountInString(event.EventMessage))
	var eventCallerSpace = strings.Repeat(" ", o.maxEventCallerLength-utf8.RuneCountInString(event.EventCaller))
	var eventTypeSpace = strings.Repeat(" ", witness.MaxEventValueLength()-utf8.RuneCountInString(event.EventType.String()))
	var eventMessageSpace = strings.Repeat(" ", o.maxEventMessageLength-utf8.RuneCountInString(event.EventMessage))
	o.mu.Unlock()

	var buf = o.bufPool.Get().([]byte)
	buf = buf[0:0]
	buf = append(buf, '\n')
	buf = o.appendTime(buf, event.EventDate)
	buf = append(buf, ' ')
	buf = base64.StdEncoding.AppendEncode(buf, event.EventID.Bytes())
	buf = append(buf, ' ')
	if o.printCaller {
		buf = append(buf, event.EventCaller...)
		buf = append(buf, eventCallerSpace...)
		buf = append(buf, ' ')
	}
	buf = event.EventType.Append(buf)
	buf = append(buf, eventTypeSpace...)
	buf = append(buf, ' ')
	buf = append(buf, event.EventMessage...)
	buf = append(buf, eventMessageSpace...)
	buf = append(buf, ' ')
	buf = append(buf, '[')
	for i, sid := range event.SpanIDs {
		if i != 0 {
			buf = append(buf, ' ')
		}
		buf = base64.StdEncoding.AppendEncode(buf, sid.Bytes())
	}
	buf = append(buf, ']')
	for _, rcd := range event.Records {
		buf = append(buf, "\n\t"...)
		buf = rcd.AppendKey(buf)
		buf = append(buf, ": \""...)
		buf = rcd.AppendValue(buf)
		buf = append(buf, "\""...)
	}

	_, _ = os.Stdout.Write(buf)
	o.bufPool.Put(buf)
}
