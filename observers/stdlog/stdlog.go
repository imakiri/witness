package stdlog

import (
	"encoding/base64"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Observer struct {
	bufPool              *sync.Pool
	mu                   *sync.Mutex
	maxEventNameLength   int
	maxEventValueLength  int
	maxEventCallerLength int
	formatter            record.Formatter
	printCaller          bool
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
		//maxEventNameLength: 8,
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

func (o *Observer) Observe(spanIDs []uuid.UUID, eventID uuid.UUID, eventDate time.Time, eventType witness.EventType, eventName string, eventCaller string, records ...witness.Record) {

	o.mu.Lock()
	o.maxEventCallerLength = max(o.maxEventCallerLength, utf8.RuneCountInString(eventCaller))
	o.maxEventNameLength = max(o.maxEventNameLength, utf8.RuneCountInString(eventName))
	//o.maxEventValueLength = max(o.maxEventValueLength, utf8.RuneCountInString(eventValue))
	var eventCallerSpace = strings.Repeat(" ", o.maxEventCallerLength-utf8.RuneCountInString(eventCaller))
	var eventTypeSpace = strings.Repeat(" ", witness.MaxEventValueLength()-utf8.RuneCountInString(eventType.String()))
	var eventNameSpace = strings.Repeat(" ", o.maxEventNameLength-utf8.RuneCountInString(eventName))
	//var eventValueSpace = strings.Repeat(" ", o.maxEventValueLength-utf8.RuneCountInString(eventValue))
	o.mu.Unlock()

	var buf = o.bufPool.Get().([]byte)
	buf = buf[0:0]
	buf = append(buf, '\n')
	buf = o.appendTime(buf, eventDate)
	buf = append(buf, ' ')
	buf = base64.StdEncoding.AppendEncode(buf, eventID.Bytes())
	buf = append(buf, ' ')
	if o.printCaller {
		buf = append(buf, eventCaller...)
		buf = append(buf, eventCallerSpace...)
		buf = append(buf, ' ')
	}
	buf = eventType.Append(buf)
	buf = append(buf, eventTypeSpace...)
	buf = append(buf, ' ')
	buf = append(buf, eventName...)
	buf = append(buf, eventNameSpace...)
	buf = append(buf, ' ')
	buf = append(buf, '[')
	for i, sid := range spanIDs {
		if i != 0 {
			buf = append(buf, ' ')
		}
		buf = base64.StdEncoding.AppendEncode(buf, sid.Bytes())
	}
	buf = append(buf, ']')
	for _, rcd := range records {
		buf = append(buf, "\n\t"...)
		buf = rcd.AppendKey(buf)
		buf = append(buf, ": \""...)
		buf = rcd.AppendValue(buf)
		buf = append(buf, "\""...)
	}

	_, _ = os.Stdout.Write(buf)
	o.bufPool.Put(buf)
}
