package audioformat_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

func detector(id string, priority int) audioformat.Detector {
	return audioformat.Detector{PluginID: id, Priority: priority, Extensions: []string{".test"}, Probe: func([]byte, int64) (audioformat.Match, bool) {
		return audioformat.Match{PluginID: "forged", Format: "test", MIME: "audio/test"}, true
	}}
}

func TestPriorityAndStableRegistrationOrderChooseOnePlugin(t *testing.T) {
	for _, test := range []struct {
		name      string
		detectors []audioformat.Detector
		want      string
	}{
		{"priority", []audioformat.Detector{detector("a", 10), detector("z", 100)}, "z"},
		{"equal priority", []audioformat.Detector{detector("z", 10), detector("a", 10)}, "z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, err := audioformat.New(test.detectors...)
			if err != nil {
				t.Fatal(err)
			}
			got, ok, err := r.Detect(strings.NewReader("content"), 7)
			if err != nil || !ok || got.PluginID != test.want {
				t.Fatalf("unexpected match: %+v %v %v", got, ok, err)
			}
		})
	}
}

func TestHeaderReadIsBoundedAndExtensionDoesNotIdentifyContent(t *testing.T) {
	d := detector("plugin", 1)
	d.Probe = func(header []byte, size int64) (audioformat.Match, bool) {
		if len(header) != audioformat.MaxHeaderBytes || size != 1<<20 {
			t.Fatalf("unbounded probe: %d %d", len(header), size)
		}
		return audioformat.Match{}, false
	}
	r, err := audioformat.New(d)
	if err != nil {
		t.Fatal(err)
	}
	reader := &boundedReader{t: t}
	if _, ok, err := r.Detect(reader, 1<<20); err != nil || ok {
		t.Fatalf("unexpected match: %v %v", ok, err)
	}
	if reader.calls != 1 {
		t.Fatalf("read count = %d", reader.calls)
	}
	if !r.SupportsExtension("renamed.TEST") || r.SupportsExtension("unknown.bin") {
		t.Fatal("invalid plugin extension candidates")
	}
	if _, ok, err := r.Detect(bytes.NewReader(nil), 0); err != nil || ok {
		t.Fatalf("empty input matched: %v %v", ok, err)
	}
}

type boundedReader struct {
	t     *testing.T
	calls int
}

func (r *boundedReader) ReadAt(data []byte, offset int64) (int, error) {
	r.calls++
	if len(data) > audioformat.MaxHeaderBytes || offset != 0 {
		r.t.Fatal("unbounded file access")
	}
	return len(data), nil
}

type failedReader struct{ err error }

func (r failedReader) ReadAt([]byte, int64) (int, error) { return 0, r.err }

func TestInvalidDescriptorsInputsAndReadFailures(t *testing.T) {
	for _, descriptors := range [][]audioformat.Detector{{{}}, {detector("same", 1), detector("same", 2)}} {
		if _, err := audioformat.New(descriptors...); err == nil {
			t.Fatal("accepted invalid detector")
		}
	}
	r, _ := audioformat.New(detector("valid", 1))
	for _, input := range []struct {
		reader io.ReaderAt
		size   int64
	}{{nil, 1}, {strings.NewReader("x"), -1}, {strings.NewReader(""), 1}} {
		if _, _, err := r.Detect(input.reader, input.size); err == nil {
			t.Fatal("accepted invalid input")
		}
	}
	failure := errors.New("denied")
	if _, _, err := r.Detect(failedReader{failure}, 1); !errors.Is(err, failure) {
		t.Fatalf("read failure lost: %v", err)
	}
}

func TestProbeCannotChangeAnotherPluginsHeader(t *testing.T) {
	first := detector("first", 10)
	first.Probe = func(header []byte, _ int64) (audioformat.Match, bool) {
		header[0] = 'X'
		return audioformat.Match{}, false
	}
	second := detector("second", 5)
	second.Probe = func(header []byte, _ int64) (audioformat.Match, bool) {
		return audioformat.Match{Format: "original", MIME: "audio/test"}, string(header) == "original"
	}
	r, err := audioformat.New(first, second)
	if err != nil {
		t.Fatal(err)
	}
	match, ok, err := r.Detect(strings.NewReader("original"), 8)
	if err != nil || !ok || match.PluginID != "second" {
		t.Fatalf("probe mutated shared input: %+v %v %v", match, ok, err)
	}
}
