package propagation

import (
	"net/http"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectExtractRoundTrip(t *testing.T) {
	rootSpan := uuid.Must(uuid.NewV7())
	currentSpan := uuid.Must(uuid.NewV7())

	headers := http.Header{}
	Inject(headers, rootSpan, currentSpan)

	require.NotEmpty(t, headers.Get(TraceparentHeader))

	traceID, parentSpanID, ok := Extract(headers)
	require.True(t, ok)

	// trace_id is the first 16 bytes of rootSpan
	assert.Equal(t, rootSpan, traceID)
	// span_id is the last 8 bytes of currentSpan, padded into the low half of a UUID
	var expected uuid.UUID
	copy(expected[8:], currentSpan[8:])
	assert.Equal(t, expected, parentSpanID)
}

func TestExtractMissingHeader(t *testing.T) {
	_, _, ok := Extract(http.Header{})
	assert.False(t, ok)
}

func TestExtractMalformed(t *testing.T) {
	cases := []string{
		"garbage",
		"00-not-hex-01",
		"00-deadbeef-deadbeef-01", // wrong lengths
		"00-00000000000000000000000000000000-zzzzzzzzzzzzzzzz-01", // bad hex in span
		"01-00000000000000000000000000000001-0000000000000001",    // wrong field count
	}
	for _, header := range cases {
		t.Run(header, func(t *testing.T) {
			h := http.Header{}
			h.Set(TraceparentHeader, header)
			_, _, ok := Extract(h)
			assert.False(t, ok)
		})
	}
}

func TestInjectFormat(t *testing.T) {
	rootSpan := uuid.UUID{0xaa, 0xbb, 0xcc, 0xdd, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b}
	currentSpan := uuid.UUID{0, 0, 0, 0, 0, 0, 0, 0, 0xde, 0xad, 0xbe, 0xef, 0xfe, 0xed, 0xfa, 0xce}

	h := http.Header{}
	Inject(h, rootSpan, currentSpan)
	assert.Equal(t, "00-aabbccdd000102030405060708090a0b-deadbeeffeedface-01", h.Get(TraceparentHeader))
}
