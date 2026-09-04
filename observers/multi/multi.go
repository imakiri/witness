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
