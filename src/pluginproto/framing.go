package pluginproto

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

type Framer struct {
	reader          *bufio.Reader
	writer          io.Writer
	mu              sync.Mutex
	maxMessageBytes int
}

func NewFramer(r io.Reader, w io.Writer) *Framer {
	return NewFramerWithLimit(r, w, MaxMessageBytes)
}

// NewFramerWithLimit is reserved for explicitly versioned internal transports.
// Provider and audio control channels continue using the default SDK bound.
func NewFramerWithLimit(r io.Reader, w io.Writer, limit int) *Framer {
	if limit < 1 {
		limit = MaxMessageBytes
	}
	return &Framer{reader: bufio.NewReaderSize(r, 4096), writer: w, maxMessageBytes: limit}
}

func (f *Framer) Read() (Envelope, error) {
	var e Envelope
	length := -1
	headerBytes := 0
	for {
		line, err := f.reader.ReadSlice('\n')
		if err != nil {
			return e, err
		}
		headerBytes += len(line)
		if headerBytes > 8192 {
			return e, fmt.Errorf("PROTOCOL_MISMATCH: header too large")
		}
		if string(line) == "\r\n" {
			break
		}
		if !strings.HasSuffix(string(line), "\r\n") {
			return e, fmt.Errorf("PROTOCOL_MISMATCH: invalid header")
		}
		key, value, ok := strings.Cut(strings.TrimSuffix(string(line), "\r\n"), ":")
		if !ok || !strings.EqualFold(key, "Content-Length") || length != -1 {
			return e, fmt.Errorf("PROTOCOL_MISMATCH: invalid content length header")
		}
		length, err = strconv.Atoi(strings.TrimSpace(value))
		if err != nil || length <= 0 || length > f.maxMessageBytes {
			return e, fmt.Errorf("PROTOCOL_MISMATCH: invalid message size")
		}
	}
	if length <= 0 {
		return e, fmt.Errorf("PROTOCOL_MISMATCH: missing content length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(f.reader, body); err != nil {
		return e, err
	}
	if !utf8.Valid(body) {
		return e, fmt.Errorf("PROTOCOL_MISMATCH: invalid UTF-8")
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return e, fmt.Errorf("PROTOCOL_MISMATCH: invalid JSON")
	}
	if e.JSONRPC != "2.0" || len(e.ID) > 80 || (e.Method != "" && (e.Result != nil || e.Error != nil)) || (e.Method == "" && (e.ID == "" || (e.Error == nil) == (e.Result == nil))) {
		return e, fmt.Errorf("PROTOCOL_MISMATCH: invalid envelope")
	}
	return e, nil
}

func (f *Framer) Write(e Envelope) error {
	e.JSONRPC = "2.0"
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if len(body) > f.maxMessageBytes {
		return fmt.Errorf("PROTOCOL_MISMATCH: message too large")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err = fmt.Fprintf(f.writer, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = f.writer.Write(body)
	return err
}
