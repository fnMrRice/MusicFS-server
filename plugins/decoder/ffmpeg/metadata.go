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

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
)

type Metadata = audiotypes.Metadata

func ProbeMetadata(ctx context.Context, ffprobePath string, input io.ReadSeeker, size int64) (metadata Metadata, probeErr error) {
	ctx = pluginlog.Context(ctx, "musicfs.plugin.decoder.ffmpeg", "")
	finish := pluginlog.Operation(ctx, "audio.metadata.probe")
	defer func() { finish(probeErr) }()
	result := Metadata{ProbeAlgorithm: "ffprobe"}
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
	out, err := toolOutput(ctx, ffprobePath, "-v", "error", "-protocol_whitelist", "http,tcp", "-format_whitelist", inputFormats, "-select_streams", "a:0", "-show_entries", "program_version=version:stream=codec_name,bit_rate,duration,sample_rate,channels,channel_layout:format=format_name,duration,bit_rate:format_tags=title,artist,album,genre,date,year", "-show_program_version", "-of", "json", "http://"+listener.Addr().String()+path)
	if err != nil {
		return result, fmt.Errorf("UNSUPPORTED: audio metadata unavailable: %w", err)
	}
	var wire struct {
		ProgramVersion struct {
			Version string `json:"version"`
		} `json:"program_version"`
		Streams []struct {
			Codec      string `json:"codec_name"`
			Duration   string `json:"duration"`
			Bitrate    string `json:"bit_rate"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
			Layout     string `json:"channel_layout"`
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
	if stream.Channels >= 1 && stream.Channels <= 32 {
		result.Channels = stream.Channels
		result.Layout = stream.Layout
	}
	if rate, err := strconv.ParseInt(stream.SampleRate, 10, 64); err == nil && rate > 0 && rate <= 1000000 {
		result.SampleRateHz = &rate
	}
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
		case "album":
			if value != "" {
				v := value
				result.Album = &v
			}
		case "genre":
			if value != "" {
				v := value
				result.Genre = &v
			}
		}
	}
	// Date wins over year deterministically; malformed tags remain unknown.
	for _, tag := range []string{"date", "year"} {
		for key, value := range wire.Format.Tags {
			if strings.EqualFold(key, tag) && result.Year == nil && len(value) >= 4 && len(value) <= 4096 {
				if year, err := strconv.ParseInt(value[:4], 10, 64); err == nil && year >= 1000 && year <= 9999 {
					result.Year = &year
				}
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
