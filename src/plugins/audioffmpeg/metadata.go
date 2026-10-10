package audioffmpeg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Metadata struct {
	Codec         *string
	Container     *string
	DurationMs    *int64
	BitrateBps    *int64
	MIME          string
	Title         *string
	Artists       []string
	BitrateOrigin string
	ProbeVersion  string
}

func ProbeMetadata(ctx context.Context, ffprobePath string, input io.ReadSeeker, size int64) (Metadata, error) {
	var result Metadata
	if size <= 0 {
		return result, fmt.Errorf("UNSUPPORTED: empty audio input")
	}
	readerAt, ok := input.(io.ReaderAt)
	if !ok {
		return result, fmt.Errorf("UNSUPPORTED: independent random input readers required")
	}
	toolInfo, err := os.Stat(ffprobePath)
	if !filepath.IsAbs(ffprobePath) || err != nil || !toolInfo.Mode().IsRegular() {
		return result, fmt.Errorf("UNSUPPORTED: invalid metadata probe executable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return result, fmt.Errorf("TEMPORARY_FAILURE: probe channel unavailable")
	}
	var random [24]byte
	rand.Read(random[:])
	path := "/" + hex.EncodeToString(random[:])
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		http.ServeContent(w, r, "input", time.Time{}, io.NewSectionReader(readerAt, 0, size))
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 2 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	go server.Serve(listener)
	defer server.Close()
	out, err := toolOutput(ctx, ffprobePath, "-v", "error", "-protocol_whitelist", "http,tcp", "-format_whitelist", inputFormats, "-select_streams", "a:0", "-show_entries", "program_version=version:stream=codec_name,bit_rate,duration:format=format_name,duration,bit_rate:format_tags=title,artist", "-show_program_version", "-of", "json", "http://"+listener.Addr().String()+path)
	if err != nil {
		return result, fmt.Errorf("UNSUPPORTED: audio metadata unavailable: %w", err)
	}
	var wire struct {
		ProgramVersion struct {
			Version string `json:"version"`
		} `json:"program_version"`
		Streams []struct {
			Codec    string `json:"codec_name"`
			Duration string `json:"duration"`
			Bitrate  string `json:"bit_rate"`
		} `json:"streams"`
		Format struct {
			Name     string            `json:"format_name"`
			Duration string            `json:"duration"`
			Bitrate  string            `json:"bit_rate"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
	}
	if json.Unmarshal([]byte(out), &wire) != nil || len(wire.Streams) == 0 || wire.Streams[0].Codec == "" || wire.ProgramVersion.Version == "" {
		return result, fmt.Errorf("UNSUPPORTED: no audio stream found")
	}
	stream := wire.Streams[0]
	result.ProbeVersion = wire.ProgramVersion.Version
	result.Codec = &stream.Codec
	result.Container = &wire.Format.Name
	duration := stream.Duration
	if duration == "" {
		duration = wire.Format.Duration
	}
	if seconds, err := strconv.ParseFloat(duration, 64); err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds >= 0 && seconds <= 86400 {
		ms := int64(math.Round(seconds * 1000))
		result.DurationMs = &ms
	}
	bitrate := stream.Bitrate
	result.BitrateOrigin = "probe"
	if bitrate == "" {
		bitrate = wire.Format.Bitrate
		result.BitrateOrigin = "estimate"
	}
	if value, err := strconv.ParseInt(bitrate, 10, 64); err == nil && value > 0 {
		result.BitrateBps = &value
	}
	for key, value := range wire.Format.Tags {
		if len(value) > 4096 {
			continue
		}
		switch strings.ToLower(key) {
		case "title":
			v := value
			result.Title = &v
		case "artist":
			if value != "" {
				result.Artists = []string{value}
			}
		}
	}
	switch {
	case wire.Format.Name == "wav":
		result.MIME = "audio/wav"
	case wire.Format.Name == "flac":
		result.MIME = "audio/flac"
	case wire.Format.Name == "mp3":
		result.MIME = "audio/mpeg"
	case wire.Format.Name == "ogg":
		result.MIME = "audio/ogg"
	case wire.Format.Name == "aiff":
		result.MIME = "audio/aiff"
	case strings.Contains(wire.Format.Name, "mp4"):
		result.MIME = "audio/mp4"
	case wire.Format.Name == "aac":
		result.MIME = "audio/aac"
	default:
		result.MIME = "application/octet-stream"
	}
	return result, nil
}
