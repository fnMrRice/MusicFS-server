package pluginproto_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestFramerFragmentedUTF8RoundTrip(t *testing.T) {
	var wire bytes.Buffer
	sender := pluginproto.NewFramer(nil, &wire)
	params := json.RawMessage(`{"query":"音乐"}`)
	for _, id := range []string{"request-1", "request-2"} {
		if err := sender.Write(pluginproto.Envelope{ID: id, Method: "search", Params: params}); err != nil {
			t.Fatal(err)
		}
	}
	receiver := pluginproto.NewFramer(iotest.OneByteReader(&wire), io.Discard)
	for _, id := range []string{"request-1", "request-2"} {
		envelope, err := receiver.Read()
		if err != nil || envelope.ID != id || envelope.Method != "search" || !bytes.Equal(envelope.Params, params) {
			t.Fatalf("fragmented frame corrupted: %+v, %v", envelope, err)
		}
	}
}

func TestFramerExplicitLimitPreservesDefaultLimit(t *testing.T) {
	payload, err := json.Marshal(strings.Repeat("x", pluginproto.MaxMessageBytes))
	if err != nil {
		t.Fatal(err)
	}
	envelope := pluginproto.Envelope{ID: "large", Result: payload}
	var wire bytes.Buffer
	if err := pluginproto.NewFramer(nil, &wire).Write(envelope); err == nil || wire.Len() != 0 {
		t.Fatal("default control channel accepted an oversized frame")
	}
	if err := pluginproto.NewFramerWithLimit(nil, &wire, 2*pluginproto.MaxMessageBytes).Write(envelope); err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), wire.Bytes()...)
	if _, err := pluginproto.NewFramer(bytes.NewReader(data), io.Discard).Read(); err == nil {
		t.Fatal("default receiver accepted an oversized frame")
	}
	got, err := pluginproto.NewFramerWithLimit(bytes.NewReader(data), io.Discard, 2*pluginproto.MaxMessageBytes).Read()
	if err != nil || !bytes.Equal(got.Result, payload) {
		t.Fatalf("explicit transport limit did not round trip: %v", err)
	}
}

func TestFramerRejectsAmbiguousOrInvalidFrames(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":"1","result":null}`
	for _, wire := range []string{
		fmt.Sprintf("Content-Length: %d\r\nContent-Length: %d\r\n\r\n%s", len(body), len(body), body),
		fmt.Sprintf("Content-Length: %d\n\n%s", len(body), body),
		"Content-Length: 0\r\n\r\n",
		"Content-Length: 1\r\n\r\n\xff",
		"Content-Length: 2\r\n\r\n{}",
	} {
		if _, err := pluginproto.NewFramer(strings.NewReader(wire), io.Discard).Read(); err == nil {
			t.Fatalf("invalid wire frame accepted: %q", wire)
		}
	}
}
