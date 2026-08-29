// Package tee
//
// Deprecated: use github.com/imakiri/witness/observers/multi
package tee

import (
	"github.com/imakiri/witness"
)

type Observer struct {
	observers []witness.Observer
}

// NewObserver
//
// Deprecated: use github.com/imakiri/witness/observers/multi.NewObserver
func NewObserver(observers ...witness.Observer) Observer {
	return Observer{observers: observers}
}

func (o Observer) Observe(event witness.Event) {
	for _, observer := range o.observers {
		observer.Observe(event)
	}
}
