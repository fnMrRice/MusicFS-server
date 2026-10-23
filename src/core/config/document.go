package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

const MaxDocumentBytes = 1 << 20

var yamlErrorLine = regexp.MustCompile(`\bline ([0-9]+):`)

type Document struct {
	Config   Config
	Root     *yaml.Node
	Raw      []byte
	Revision string
	Path     string
}

func Load(path string, overrides map[string]string) (Config, error) {
	doc, err := ReadDocument(path, overrides)
	if err != nil {
		return Config{}, err
	}
	return doc.Config, nil
}

func ReadDocument(path string, overrides map[string]string) (*Document, error) {
	return readDocument(path, overrides, nil)
}

func readDocument(path string, overrides map[string]string, expected *Config) (*Document, error) {
	if _, err := documentPath(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read configuration file")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxDocumentBytes+1))
	if err != nil {
		return nil, errors.New("cannot read configuration file")
	}
	return parseDocument(raw, path, overrides, expected)
}

func ParseDocument(raw []byte, path string, overrides map[string]string) (*Document, error) {
	return parseDocument(raw, path, overrides, nil)
}

func parseDocument(raw []byte, path string, overrides map[string]string, expected *Config) (*Document, error) {
	abs, err := documentPath(path)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > MaxDocumentBytes {
		return nil, errors.New("configuration must contain 1 to 1048576 bytes")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if match := yamlErrorLine.FindStringSubmatch(err.Error()); match != nil {
			return nil, errors.New("invalid configuration YAML syntax at line " + match[1])
		}
		return nil, errors.New("invalid configuration YAML syntax")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("configuration must contain one mapping document")
	}
	if decoder.Decode(new(yaml.Node)) != io.EOF {
		return nil, errors.New("configuration must contain exactly one YAML document")
	}
	root := document.Content[0]
	if err := validateNodes(root, 1); err != nil {
		return nil, err
	}
	normalizeIdentityNodes(root)
	if err := discoverBundle(root, filepath.Dir(abs)); err != nil {
		return nil, err
	}
	if err := validateDocumentShape(root); err != nil {
		return nil, err
	}
	c := defaults()
	if err := root.Decode(&c); err != nil {
		return nil, errors.New("invalid configuration field value")
	}
	if err := applyOverrides(&c, overrides); err != nil {
		return nil, err
	}
	_, mediaEnvironment := os.LookupEnv("MUSICFS_MEDIA_ROOT")
	_, mediaOverride := overrides["MEDIA_ROOT"]
	if c.MediaRoot == "" && (yamlField(root, "mediaRoot") != nil || mediaEnvironment || mediaOverride) {
		return nil, errors.New("mediaRoot must be a nonempty path when specified")
	}
	if c.Logging.Level == "" {
		return nil, errors.New("logging.level must be trace, debug, info, warn or error")
	}
	resolvePaths(&c, filepath.Dir(abs))
	c.NormalizeRuntime()
	if err := c.Logging.Validate(); err != nil {
		return nil, err
	}
	if err := c.Storage.Validate(); err != nil {
		return nil, err
	}
	if expected != nil && !SameStartupSettings(*expected, c) {
		return nil, errors.New("storage, data roots, logging or DSP configuration changed; restart is required")
	}
	if err := loadLocalDirectoryDefaults(context.Background(), &c); err != nil {
		return nil, err
	}
	markPendingConfiguration(&c, bundlePath(root, filepath.Dir(abs)))
	for pi := range c.Plugins {
		if !pluginconfig.SupportsDirectories(c.Plugins[pi].PluginID) {
			continue
		}
		for ii := range c.Plugins[pi].Instances {
			i := &c.Plugins[pi].Instances[ii]
			if i.Permissions != nil && (len(i.Config.MusicRoots) == 0 || validateRootAuthority(*i, i.Config.MusicRoots) != nil) {
				i.PendingConfiguration = true
			}
		}
	}
	c.SourcePath, c.SourceOverrides = abs, maps.Clone(overrides)
	hash := sha256.Sum256(raw)
	doc := &Document{Config: c, Root: &document, Raw: bytes.Clone(raw), Revision: hex.EncodeToString(hash[:]), Path: abs}
	doc.Revision = localDirectoryRevision(c, doc.Revision)
	// Validate cross-feature references against effective settings, including
	// writeback grants for roots created through the API rather than YAML.
	if err := loadPluginSettings(context.Background(), doc); err != nil {
		return nil, err
	}
	if err := doc.Config.Validate(); err != nil {
		return nil, err
	}
	return doc, nil
}

func documentPath(path string) (string, error) {
	if path == "" || (strings.ToLower(filepath.Ext(path)) != ".yaml" && strings.ToLower(filepath.Ext(path)) != ".yml") {
		return "", errors.New("configuration path must name a .yaml or .yml file")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", errors.New("invalid configuration path")
	}
	return abs, nil
}

func applyOverrides(c *Config, overrides map[string]string) error {
	fields := map[string]*string{"LISTEN": &c.Listen, "DATA_ROOT": &c.DataRoot, "MEDIA_ROOT": &c.MediaRoot, "CACHE_ROOT": &c.CacheRoot, "WEB_ROOT": &c.WebRoot, "ADMIN_TOKEN": &c.AdminToken, "LOG_LEVEL": &c.Logging.Level}
	for key := range overrides {
		if _, ok := fields[key]; !ok {
			return errors.New("unsupported configuration override")
		}
	}
	for key, target := range fields {
		if value, ok := os.LookupEnv("MUSICFS_" + key); ok {
			*target = value
		}
		if value, ok := overrides[key]; ok {
			*target = value
		}
	}
	return nil
}
