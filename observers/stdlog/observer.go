package stdlog

import (
	"context"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
	"log"
	"strings"
)

type Observer struct {
	maxEventNameLength  int
	maxEventValueLength int
	formatter           record.Formatter
}

func NewObserver() *Observer {
	return &Observer{
		//maxEventNameLength: 8,
		//maxEventValueLength: 8,
		formatter: record.DefaultFormatter{},
	}
}

func (o *Observer) Observe(ctx context.Context, spanID uuid.UUID, eventType witness.EventType, eventName string, eventValue string, records ...witness.Record) {
	o.maxEventNameLength = max(o.maxEventNameLength, len(eventName))
	o.maxEventValueLength = max(o.maxEventValueLength, len(eventValue))
	var eventTypeSpace = strings.Repeat(" ", witness.MaxValueLength()-len(eventType.String()))
	var eventNameSpace = strings.Repeat(" ", o.maxEventNameLength-len(eventName))
	var eventValueSpace = strings.Repeat(" ", o.maxEventValueLength-len(eventValue))
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
	log.Println("spanID:", spanID, "eventType:", eventType, eventTypeSpace, "eventName:", eventName, eventNameSpace, "eventValue:", eventValue, eventValueSpace, "records:", string(stringRecords))
}
