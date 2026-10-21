package pluginlog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
)

func TestOperationUsesScopedIdentityAndSafeErrorCode(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: pluginlog.LevelTrace})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ctx := pluginlog.Context(context.Background(), "musicfs.plugin.source.localfiles", "instance-test")
	finish := pluginlog.Operation(ctx, "provider.scan")
	finish(fault.Fail("ACCESS_DENIED", "password=secret C:\\private\\music.flac"))
	if strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "private") {
		t.Fatal("log contains sensitive error text")
	}
	decoder := json.NewDecoder(&output)
	for i, event := range []string{"plugin.operation.started", "plugin.operation.failed"} {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["pluginId"] != "musicfs.plugin.source.localfiles" || record["instanceId"] != "instance-test" || record["event"] != event || record["operation"] != "provider.scan" {
			t.Fatalf("unexpected record %d: %v", i, record)
		}
		if i == 1 && (record["level"] != "WARN" || record["errorCode"] != "ACCESS_DENIED") {
			t.Fatalf("unexpected failure severity: %v", record)
		}
	}
}

func TestOperationDefaultInfoHidesTraceAndDebug(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ctx := pluginlog.Context(context.Background(), "musicfs.plugin.decoder.ncm", "")
	pluginlog.Operation(ctx, "ncm.open")(nil)
	pluginlog.Operation(ctx, "ncm.open")(context.Canceled)
	if output.Len() != 0 {
		t.Fatalf("normal or canceled operations leaked at info level: %s", output.String())
	}
}

func TestErrorCodeNeverReturnsUnknownText(t *testing.T) {
	for _, item := range []struct {
		err  error
		want string
	}{
		{nil, ""}, {context.Canceled, "CANCELED"}, {fmt.Errorf("wrapped: %w", context.DeadlineExceeded), "DEADLINE_EXCEEDED"},
		{io.EOF, "EOF"}, {errors.New("PROTOCOL_MISMATCH: private content"), "PROTOCOL_MISMATCH"},
		{errors.New("token-secret: private content"), "INTERNAL_ERROR"}, {fault.Fail("token-secret", "private content"), "INTERNAL_ERROR"},
	} {
		if got := pluginlog.ErrorCode(item.err); got != item.want {
			t.Fatalf("got code %q, want %q", got, item.want)
		}
	}
}

func TestExternalDiagnosticsKeepSeverityAndProtocolOutput(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	previous, stderr, stdout := slog.Default(), os.Stderr, os.Stdout
	os.Stderr = writer
	t.Cleanup(func() { os.Stderr = stderr; slog.SetDefault(previous) })
	pluginlog.External()
	ctx := pluginlog.Context(context.Background(), "musicfs.dsp.gain", "")
	pluginlog.Logger(ctx).Debug("DSP gain node configured")
	pluginlog.Operation(ctx, "dsp.process")(nil)
	if os.Stdout != stdout {
		t.Fatal("plugin logging changed protocol output")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(reader)
	for _, event := range []string{"plugin.diagnostic", "plugin.operation.started", "plugin.operation.completed"} {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["event"] != event {
			t.Fatalf("unexpected event: %v", record)
		}
		if event == "plugin.diagnostic" && record["level"] != "DEBUG" {
			t.Fatalf("unexpected diagnostic severity: %v", record)
		}
	}
}
