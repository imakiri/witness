package propagation

import (
	"net/http"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectExtractRoundTrip(t *testing.T) {
	currentSpan := uuid.Must(uuid.NewV7())

	headers := http.Header{}
	Inject(headers, currentSpan)

	require.NotEmpty(t, headers.Get(TraceparentHeader))

	parentSpanID, ok := Extract(headers)
	require.True(t, ok)

	// The whole span_id survives: it rides in the 16-byte trace-id field,
	// which witness has no trace_id to spend.
	assert.Equal(t, currentSpan, parentSpanID)
}

func TestExtractMissingHeader(t *testing.T) {
	_, ok := Extract(http.Header{})
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
			_, ok := Extract(h)
			assert.False(t, ok)
		})
	}
}

func TestInjectFormat(t *testing.T) {
	span := uuid.UUID{0xaa, 0xbb, 0xcc, 0xdd, 0x00, 0x01, 0x02, 0x03, 0xde, 0xad, 0xbe, 0xef, 0xfe, 0xed, 0xfa, 0xce}

	h := http.Header{}
	Inject(h, span)
	// trace-id field is the whole span_id; parent-id field repeats its low half.
	assert.Equal(t, "00-aabbccdd00010203deadbeeffeedface-deadbeeffeedface-01", h.Get(TraceparentHeader))
}
