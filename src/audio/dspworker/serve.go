package dspworker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/fnMrRice/MusicFS-server/src/audio"
)

// Serve exposes one version-1, frame-preserving node over stdin/stdout. Plugins
// must write diagnostics to stderr; stdout belongs exclusively to this protocol.
// Node implementations must have zero latency and no output tail.
func Serve(ctx context.Context, input io.Reader, output io.Writer, name string, factory audio.Factory) error {
	if !validName.MatchString(name) || factory == nil {
		return fmt.Errorf("invalid DSP worker identity")
	}
	kind, payload, err := readFrame(input)
	if err != nil {
		return err
	}
	var request hello
	if kind != frameHello || json.Unmarshal(payload, &request) != nil || request.ProtocolVersion != ProtocolVersion || request.NodeVersion != audio.NodeVersion || request.Name != name || request.Format != audio.Canonical {
		return fmt.Errorf("PROTOCOL_MISMATCH: invalid DSP handshake")
	}
	node, err := factory(request.Params)
	if err != nil {
		return fail(output, err)
	}
	defer node.Close()
	if err = node.Configure(request.Format); err != nil {
		return fail(output, err)
	}
	stateless, ok := node.(interface{ Stateless() bool })
	if node.DelayFrames() != 0 || !ok || !stateless.Stateless() {
		return fail(output, fmt.Errorf("UNSUPPORTED: DSP v1 requires stateless processing with zero latency"))
	}
	err = writeFrame(output, frameHello, marshal(ready{ProtocolVersion: ProtocolVersion, Name: name, NodeVersion: audio.NodeVersion, Format: audio.Canonical, PreservesTiming: true, Stateless: true}))
	for err == nil {
		if err = ctx.Err(); err != nil {
			return err
		}
		kind, payload, err = readFrame(input)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		var response []byte
		switch kind {
		case frameProcess:
			var block *audio.Block
			block, err = decodeBlock(payload)
			if err == nil {
				err = node.Process(block)
			}
			if err == nil {
				response, err = encodeBlock(block)
			}
		case frameReset, frameDrain:
			if len(payload) != 0 {
				err = fmt.Errorf("PROTOCOL_MISMATCH: unexpected DSP control payload")
			} else if kind == frameReset {
				err = node.Reset()
			} else {
				err = node.Drain()
			}
		default:
			err = fmt.Errorf("PROTOCOL_MISMATCH: unsupported DSP operation")
		}
		if err != nil {
			return fail(output, err)
		}
		err = writeFrame(output, kind, response)
	}
	return err
}

func fail(output io.Writer, err error) error {
	writeFrame(output, frameError, marshal(map[string]string{"error": "DSP operation failed"}))
	return err
}
