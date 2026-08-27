package tee

import (
	"github.com/imakiri/witness"
)

type Observer struct {
	observers []witness.Observer
}

func NewObserver(observers ...witness.Observer) Observer {
	return Observer{observers: observers}
}

func (o Observer) Observe(event witness.Event) {
	for _, observer := range o.observers {
		observer.Observe(event)
	}
}
