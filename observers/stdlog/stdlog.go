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
}

func NewObserver() *Observer {
	var bufPool = new(sync.Pool)
	bufPool.New = func() any {
		return make([]byte, 0, 256)
	}
	return &Observer{
		bufPool: bufPool,
		mu:      new(sync.Mutex),
		//maxEventNameLength: 8,
		maxEventValueLength: 8,
		formatter:           record.DefaultFormatter{},
	}
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
	buf = buf[:0]
	buf = append(buf, '\n')
	buf = eventDate.AppendFormat(buf, time.RFC3339Nano)
	buf = append(buf, ' ')
	buf = base64.StdEncoding.AppendEncode(buf, eventID.Bytes())
	buf = append(buf, ' ')
	buf = append(buf, eventCaller...)
	buf = append(buf, eventCallerSpace...)
	buf = append(buf, ' ')
	buf = append(buf, eventType.String()...)
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
		buf = append(buf, rcd.Name()...)
		buf = append(buf, ": \""...)
		buf = append(buf, rcd.String()...)
		buf = append(buf, "\""...)
	}

	_, _ = os.Stdout.Write(buf)
	o.bufPool.Put(buf)
}
