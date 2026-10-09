package plugins_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type byteReader struct{ io.Reader }

func (r byteReader) Read(b []byte) (int, error) { return r.Reader.Read(b[:1]) }

func TestControlFramingUTF8AndConcatenatedMessages(t *testing.T) {
	var out bytes.Buffer
	w := pluginproto.NewFramer(nil, &out)
	for _, id := range []string{"h-1", "h-2"} {
		if err := w.Write(pluginproto.Envelope{ID: id, Method: "provider.scan", Params: json.RawMessage(`{"name":"中文"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	r := pluginproto.NewFramer(byteReader{&out}, nil)
	for _, id := range []string{"h-1", "h-2"} {
		e, err := r.Read()
		if err != nil || e.ID != id || !strings.Contains(string(e.Params), "中文") {
			t.Fatalf("envelope=%+v error=%v", e, err)
		}
	}
	if _, err := r.Read(); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestControlFramingRejectsInvalidLengthsAndTruncation(t *testing.T) {
	for _, input := range []string{"Content-Length: 1048577\r\n\r\n", "Content-Length: -1\r\n\r\n", "Content-Length: 20\r\n\r\n{}", "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}", "\r\n", "Content-Length: 2\r\n\r\n\xff\xff"} {
		if _, err := pluginproto.NewFramer(strings.NewReader(input), nil).Read(); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}
