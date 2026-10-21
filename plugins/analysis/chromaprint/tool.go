package chromaprint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type boundedBuffer struct {
	data     []byte
	limit    int
	exceeded bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-len(b.data) {
		b.exceeded = true
		return 0, fmt.Errorf("PROTOCOL_MISMATCH: tool output exceeds limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func validSHA(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 32 && strings.ToLower(value) == value
}
func verifyTool(path, wanted string) error {
	if !filepath.IsAbs(path) || !validSHA(wanted) {
		return fmt.Errorf("INVALID_ARGUMENT: fingerprint tool requires an absolute path and SHA256")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("UNSUPPORTED: fingerprint executable unavailable")
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return fmt.Errorf("UNSUPPORTED: invalid fingerprint executable")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return fmt.Errorf("UNSUPPORTED: fingerprint executable could not be verified")
	}
	if hex.EncodeToString(h.Sum(nil)) != wanted {
		return fmt.Errorf("ACCESS_DENIED: fingerprint executable checksum mismatch")
	}
	return nil
}

func command(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	configureProcess(cmd)
	cmd.WaitDelay = time.Second
	cmd.Env = []string{}
	for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP"} {
		if value := os.Getenv(key); value != "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	return cmd
}

func probeTool(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := command(ctx, path, "-version")
	stdout, stderr := &boundedBuffer{limit: 8192}, &boundedBuffer{limit: 16384}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("UNSUPPORTED: fingerprint executable failed to start")
	}
	cleanup, err := containProcess(cmd.Process.Pid)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return "", fmt.Errorf("UNSUPPORTED: fingerprint process containment unavailable")
	}
	defer cleanup()
	if err = cmd.Wait(); err != nil || stdout.exceeded || stderr.exceeded {
		return "", fmt.Errorf("UNSUPPORTED: fingerprint tool version probe failed")
	}
	line := strings.TrimSpace(string(stdout.data))
	if len(line) > 512 || strings.ContainsAny(line, "\r\n") || !strings.HasPrefix(line, "fpcalc version 1.6.1 (") || !strings.HasSuffix(line, ")") {
		return "", fmt.Errorf("UNSUPPORTED: fingerprint tool must be Chromaprint 1.6.1")
	}
	return line, nil
}
