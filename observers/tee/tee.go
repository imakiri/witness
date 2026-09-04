// Package tee
//
// Deprecated: use github.com/imakiri/witness/observers/multi
package tee

import (
	"github.com/imakiri/witness/core"
)

type Observer struct {
	observers []core.Observer
}

// NewObserver
//
// Deprecated: use github.com/imakiri/witness/observers/multi.NewObserver
func NewObserver(observers ...core.Observer) Observer {
	return Observer{observers: observers}
}

func (o Observer) Observe(event core.Event) {
	for _, observer := range o.observers {
		observer.Observe(event)
	}
}
