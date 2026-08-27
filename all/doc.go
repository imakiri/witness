// Package all is a convenience meta-package: depending on it via a single
// `go get github.com/imakiri/witness/all` transitively pulls every observer
// and adapter shipped in this repository. Import the individual packages
// from their original paths to use them.
package all

import (
	_ "github.com/imakiri/witness"
	_ "github.com/imakiri/witness/adapters/log"
	_ "github.com/imakiri/witness/observers/otlp"
	_ "github.com/imakiri/witness/observers/postgres"
	_ "github.com/imakiri/witness/observers/prometheus"
	_ "github.com/imakiri/witness/observers/stdlog"
	_ "github.com/imakiri/witness/observers/tee"
)
