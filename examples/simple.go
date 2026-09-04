package main

import (
	"context"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/stdlog"
	"github.com/imakiri/witness/printers"
	"github.com/imakiri/witness/record"
	"log"
	"net/http"
)

func main() {

	// create observer instance
	printer, err := printers.NewPretty()
	if err != nil {
		log.Fatalln("printers.NewPretty failed with error:", err)
	}
	observer, err := stdlog.NewObserver(printer)
	if err != nil {
		log.Fatalln("stdlog.NewObserver failed with error:", err)
	}
	// create root span
	var ctx, finish = witness.Instance(context.Background(), observer, "example.simple", "1")
	defer finish()

	var i = 10
	var j = Foo(ctx, i)
	_ = j

	j = Bar(ctx, i)

	var client = new(http.Client)

	request, err := http.NewRequest(http.MethodGet, "https://google.com", nil)
	if err != nil {
		log.Fatalln("http.NewRequest failed with error:", err)
	}

	// The send mints the message's span_id and returns it for the carrier.
	msgID := witness.ExternalMessageSent(ctx, "google request")
	request.Header.Set("X-Message", msgID.String())
	response, err := client.Do(request)
	if err != nil {
		log.Fatalln("client.Do(request) failed with error:", err)
	}
	// Both halves reference the same span_id from this process's own span;
	// neither enters it.
	witness.ExternalMessageReceived(ctx, msgID, "google response", record.Int("status_code", response.StatusCode))
	if response.StatusCode != http.StatusOK {
		log.Fatalln("client.Do(request) failed with code:", response.StatusCode)
	}
}

func Foo(ctx context.Context, i int) (j int) {
	ctx, finish := witness.Span(ctx, "Foo", record.Int("i", i))
	defer func() { finish(record.Int("j", j)) }()

	for i < 17 {
		select {
		case <-ctx.Done():
			return i
		default:
			witness.Info(ctx, "Foo: work", record.Int("i", i))
			i *= i
		}
	}
	return i
}

func Bar(ctx context.Context, i int) (j int) {
	ctx, finish := witness.Span(ctx, "Bar", record.Int("i", i))
	// The records must be built inside a closure: a deferred call evaluates
	// its arguments at the `defer` statement, so `record.Int("j", j)` written
	// directly here would always report j = 0.
	defer func() { finish(record.Int("j", j)) }()
	return i * i
}
