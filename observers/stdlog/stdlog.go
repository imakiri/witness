package stdlog

import (
	"context"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
	"log"
	"strings"
	"unicode/utf8"
)

type Observer struct {
	maxEventNameLength   int
	maxEventValueLength  int
	maxEventCallerLength int
	formatter            record.Formatter
}

func NewObserver() *Observer {
	return &Observer{
		//maxEventNameLength: 8,
		//maxEventValueLength: 8,
		formatter: record.DefaultFormatter{},
	}
}

func (o *Observer) Observe(ctx context.Context, spanID uuid.UUID, eventType witness.EventType, eventName string, eventCaller string, records ...witness.Record) {
	o.maxEventCallerLength = max(o.maxEventCallerLength, utf8.RuneCountInString(eventCaller))
	o.maxEventNameLength = max(o.maxEventNameLength, utf8.RuneCountInString(eventName))
	//o.maxEventValueLength = max(o.maxEventValueLength, utf8.RuneCountInString(eventValue))
	var eventCallerSpace = strings.Repeat(" ", o.maxEventCallerLength-utf8.RuneCountInString(eventCaller))
	var eventTypeSpace = strings.Repeat(" ", witness.MaxValueLength()-utf8.RuneCountInString(eventType.String()))
	var eventNameSpace = strings.Repeat(" ", o.maxEventNameLength-utf8.RuneCountInString(eventName))
	//var eventValueSpace = strings.Repeat(" ", o.maxEventValueLength-utf8.RuneCountInString(eventValue))
	var stringRecords []byte
	stringRecords = append(stringRecords, "["...)
	for _, r := range records {
		stringRecords = append(stringRecords, r.Name()...)
		stringRecords = append(stringRecords, ": \""...)
		stringRecords = append(stringRecords, r.String()...)
		stringRecords = append(stringRecords, "\", "...)
	}
	stringRecords = stringRecords[:max(len(stringRecords)-2, 1)]
	stringRecords = append(stringRecords, "]"...)
	log.Println(eventCaller, eventCallerSpace, "spanID:", spanID, "eventType:", eventType, eventTypeSpace, "eventName:", eventName, eventNameSpace, "records:", string(stringRecords))
}
