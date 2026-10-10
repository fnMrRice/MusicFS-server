// Command release builds a private installation bundle from this repository.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var programs = []string{"musicfs", "musicfs-plugin-local", "musicfs-plugin-audio-ffmpeg", "musicfs-plugin-ncm", "musicfs-plugin-scraper-local", "musicfs-plugin-netease", "musicfs-plugin-qqmusic", "musicfs-plugin-applemusic"}

type builder struct {
	root, out, target, arch, web string
	env                          []string
}

type manifest struct {
	Version        string       `json:"version"`
	Revision       string       `json:"revision"`
	Dirty          bool         `json:"dirty"`
	OS             string       `json:"os"`
	Arch           string       `json:"arch"`
	GoVersion      string       `json:"goVersion"`
	BuiltAt        time.Time    `json:"builtAt"`
	APIVersion     string       `json:"apiVersion"`
	PluginProtocol string       `json:"pluginProtocol"`
	SchemaVersion  int          `json:"schemaVersion"`
	Programs       []string     `json:"programs"`
	Components     []component  `json:"components"`
	ExternalTools  []string     `json:"externalTools"`
	Dependencies   []dependency `json:"dependencies"`
	GoLicenses     []string     `json:"goLicenses"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	b := builder{}
	flag.StringVar(&b.target, "os", runtime.GOOS, "Target OS: windows or linux")
	flag.StringVar(&b.arch, "arch", "amd64", "Target architecture (validated: amd64)")
	flag.StringVar(&b.out, "output", "", "New installation bundle directory")
	flag.StringVar(&b.web, "web-root", "", "Optional separately built Web artifact directory")
	allowDirty := flag.Bool("allow-dirty", false, "Mark an uncommitted validation build; not a release")
	flag.Parse()
	if flag.NArg() != 0 || (b.target != "windows" && b.target != "linux") || b.arch != "amd64" || b.out == "" {
		return errors.New("provide --output, --os windows|linux and --arch amd64")
	}
	var err error
	b.root, err = os.Getwd()
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(b.root, "src", "app", "musicfs", "main.go")); err != nil {
		return errors.New("run from the Server repository root")
	}
	b.out, err = filepath.Abs(b.out)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(b.out); !os.IsNotExist(err) {
		return errors.New("output directory must not exist")
	}
	if b.web != "" {
		b.web, err = filepath.Abs(b.web)
		if err != nil {
			return err
		}
		if _, err = os.Stat(filepath.Join(b.web, "index.html")); err != nil {
			return errors.New("Web artifact requires index.html")
		}
	}
	b.env = targetEnvironment(b.target, b.arch)
	rev, err := b.command("git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	status, err := b.command("git", "status", "--porcelain")
	if err != nil {
		return err
	}
	dirty := strings.TrimSpace(string(status)) != ""
	if dirty && !*allowDirty {
		return errors.New("working tree is dirty; commit the step or use --allow-dirty for validation")
	}
	if err = os.MkdirAll(filepath.Dir(b.out), 0700); err != nil {
		return err
	}
	if err = os.Mkdir(b.out, 0700); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(b.out, ".incomplete"), []byte("Build has not completed.\n"), 0600); err != nil {
		return err
	}
	for _, name := range programs {
		file := name
		if b.target == "windows" {
			file += ".exe"
		}
		if _, err = b.command("go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", filepath.Join(b.out, "bin", file), "./src/app/"+name); err != nil {
			return err
		}
	}
	if err = b.documents(); err != nil {
		return err
	}
	deps, err := b.dependencies()
	if err != nil {
		return err
	}
	goLicenses, err := b.toolchainLicenses()
	if err != nil {
		return err
	}
	if err = b.configuration(); err != nil {
		return err
	}
	if b.web != "" {
		if err = b.copyWeb(); err != nil {
			return err
		}
	}
	version, err := b.command("go", "version")
	if err != nil {
		return err
	}
	m := manifest{Version: "0.1.0", Revision: strings.TrimSpace(string(rev)), Dirty: dirty, OS: b.target, Arch: b.arch, GoVersion: strings.TrimSpace(string(version)), BuiltAt: time.Now().UTC(), APIVersion: "1", PluginProtocol: "1.0", Programs: programs, ExternalTools: []string{"FFmpeg/ffprobe: external, libmp3lame required", "Chromaprint fpcalc 1.6.1: external, optional"}, Dependencies: deps, GoLicenses: goLicenses}
	m.Components = components()
	m.SchemaVersion, err = b.schemaVersion()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(b.out, "release.json"), append(data, '\n'), 0600); err != nil {
		return err
	}
	if err = b.checksums(); err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(b.out, ".incomplete")); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"directory": b.out, "os": b.target, "arch": b.arch, "dirty": dirty, "programs": len(programs)})
}

func targetEnvironment(target, arch string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" {
			env = append(env, entry)
		}
	}
	return append(env, "GOOS="+target, "GOARCH="+arch, "CGO_ENABLED=0")
}

func (b builder) command(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = b.root
	cmd.Env = b.env
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w\n%s", name, err, output)
	}
	return output, nil
}
