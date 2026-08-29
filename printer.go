package witness

import "io"

type PrintFlags uint64

const (
	PrintNone    PrintFlags = 0
	PrintTime    PrintFlags = 1 << 0
	PrintEventID PrintFlags = 1 << 1
	PrintCaller  PrintFlags = 1 << 2
	PrintSpanIDs PrintFlags = 1 << 3
	PrintRecords PrintFlags = 1 << 4
	PrintCR      PrintFlags = 1 << 5
	PrintLF      PrintFlags = 1 << 6
	PrintAll                = ^PrintFlags(0)
)

type Printer interface {
	Print(writer io.Writer, event Event, flags PrintFlags)
}

type Appender interface {
	Append(dst []byte, event Event, flags PrintFlags) []byte
}
