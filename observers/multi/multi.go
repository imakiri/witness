package multi

import (
	"github.com/imakiri/witness/core"
)

type Observer struct {
	observers []core.Observer
}

func NewObserver(observers ...core.Observer) Observer {
	return Observer{observers: observers}
}

func (o Observer) Observe(event core.Event) {
	for _, observer := range o.observers {
		observer.Observe(event)
	}
}

// Accepts is true as soon as one member accepts: the event has to be built
// for that one, and the members that decline it drop it themselves. A member
// that does not implement core.EventTypeFilter accepts everything, so a fan-out
// containing one unfiltered observer accepts everything — which is correct,
// and worth knowing before wondering why a filter downstream saved nothing.
func (o Observer) Accepts(eventType core.EventType) bool {
	for _, observer := range o.observers {
		if core.Accepts(observer, eventType) {
			return true
		}
	}
	return false
}
