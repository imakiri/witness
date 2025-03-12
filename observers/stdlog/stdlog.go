package stdlog

import (
	"context"
	"encoding/base64"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
	"log"
	"strings"
	"sync"
	"unicode/utf8"
)

type Observer struct {
	mu                   *sync.Mutex
	maxEventNameLength   int
	maxEventValueLength  int
	maxEventCallerLength int
	formatter            record.Formatter
}

func NewObserver() *Observer {
	return &Observer{
		mu: new(sync.Mutex),
		//maxEventNameLength: 8,
		maxEventValueLength: 8,
		formatter:           record.DefaultFormatter{},
	}
}

func (o *Observer) Observe(ctx context.Context, spanIDs []uuid.UUID, eventType witness.EventType, eventName string, eventCaller string, records ...witness.Record) {

	o.mu.Lock()
	o.maxEventCallerLength = max(o.maxEventCallerLength, utf8.RuneCountInString(eventCaller))
	o.maxEventNameLength = max(o.maxEventNameLength, utf8.RuneCountInString(eventName))
	//o.maxEventValueLength = max(o.maxEventValueLength, utf8.RuneCountInString(eventValue))
	var eventCallerSpace = strings.Repeat(" ", o.maxEventCallerLength-utf8.RuneCountInString(eventCaller))
	var eventTypeSpace = strings.Repeat(" ", witness.MaxEventValueLength()-utf8.RuneCountInString(eventType.String()))
	var eventNameSpace = strings.Repeat(" ", o.maxEventNameLength-utf8.RuneCountInString(eventName))
	//var eventValueSpace = strings.Repeat(" ", o.maxEventValueLength-utf8.RuneCountInString(eventValue))
	o.mu.Unlock()

	var stringRecords strings.Builder
	for _, r := range records {
		stringRecords.WriteString("\n\t")
		stringRecords.WriteString(r.Name())
		stringRecords.WriteString(": \"")
		stringRecords.WriteString(r.String())
		stringRecords.WriteString("\"")
	}
	var stringSpanIDs string
	for i := range spanIDs {
		stringSpanIDs += base64.StdEncoding.EncodeToString(spanIDs[i].Bytes())
		stringSpanIDs += " "
	}
	stringSpanIDs = stringSpanIDs[:len(stringSpanIDs)-1]
	log.Printf("%s %s[%s%s] %s%s [%s]%s %s", eventCaller, eventCallerSpace, stringSpanIDs, strings.Repeat(" ", 25*max(2-len(spanIDs), 0)),
		eventType, eventTypeSpace, eventName, eventNameSpace, stringRecords.String())
}
