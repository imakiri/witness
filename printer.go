package witness

import "io"

type PrintFlags uint64

const (
	PrintTime PrintFlags = 1 << iota
	PrintEventID
	PrintCaller
	PrintSpanIDs
	PrintRecords
	PrintCR
	PrintLF
)

type Printer interface {
	Print(writer io.Writer, event Event, flags PrintFlags)
}

type Appender interface {
	Append(dst []byte, event Event, flags PrintFlags) []byte
}
