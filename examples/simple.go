package main

import (
	"context"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/stdlog"
	"github.com/imakiri/witness/record"
	"log"
	"net/http"
)

func main() {
	witness.EnableDebug()

	// create observer instance
	var observer witness.Observer = stdlog.NewObserver()
	// create root span
	var ctx, finish = witness.Instance(context.Background(), observer, "example.simple", "1")
	defer finish()

	var i = 10
	var j = Foo(ctx, i)
	_ = j

	j = Bar(ctx, i)

	var client = new(http.Client)

	var msgID = uuid.Must(uuid.NewV7())
	var request, err = http.NewRequest(http.MethodGet, "https://google.com", nil)
	if err != nil {
		log.Fatalln("http.NewRequest failed with error:", err)
	}
	request.Header.Set("X-Message", msgID.String())

	witness.ExternalMessageSent(ctx, msgID, "google request")
	response, err := client.Do(request)
	if err != nil {
		log.Fatalln("client.Do(request) failed with error:", err)
	}
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
	defer finish(record.Int("j", j))
	return i * i
}
