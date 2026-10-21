package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/gain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

type helperNode struct {
	audio.Node
	mode string
}

func (h *helperNode) Stateless() bool { return h.mode != "stateful" }
func (h *helperNode) DelayFrames() int64 {
	if h.mode == "delayed" {
		return 1
	}
	return 0
}
func (h *helperNode) Process(block *audio.Block) error {
	switch h.mode {
	case "hang":
		time.Sleep(time.Hour)
	case "crash":
		os.Exit(19)
	case "retime":
		block.PositionFrames++
	}
	return h.Node.Process(block)
}

func main() {
	mode := os.Args[1]
	if mode == "version" || mode == "missing-version" || mode == "blank-version" || mode == "invalid-version" {
		var header [12]byte
		if _, err := io.ReadFull(os.Stdin, header[:]); err != nil {
			os.Exit(3)
		}
		if _, err := io.CopyN(io.Discard, os.Stdin, int64(binary.LittleEndian.Uint32(header[8:]))); err != nil {
			os.Exit(3)
		}
		response := map[string]any{"protocolVersion": 1, "pluginVersion": "0.1.0", "name": "test.gain", "nodeVersion": audio.NodeVersion, "format": audio.Canonical, "preservesTiming": true, "stateless": true}
		switch mode {
		case "version":
			response["protocolVersion"] = 2
		case "missing-version":
			delete(response, "pluginVersion")
		case "blank-version":
			response["pluginVersion"] = " "
		case "invalid-version":
			response["pluginVersion"] = "1.2"
		}
		body, _ := json.Marshal(response)
		copy(header[:4], "MFD1")
		binary.LittleEndian.PutUint32(header[4:], 1)
		binary.LittleEndian.PutUint32(header[8:], uint32(len(body)))
		os.Stdout.Write(header[:])
		os.Stdout.Write(body)
		io.Copy(io.Discard, os.Stdin)
		return
	}
	err := dspworker.Serve(context.Background(), os.Stdin, os.Stdout, "test.gain", "0.1.0", func(params map[string]float64) (audio.Node, error) {
		node, err := dsp.NewGain(params["gainDB"])
		return &helperNode{Node: node, mode: mode}, err
	})
	if err != nil {
		os.Exit(2)
	}
}
