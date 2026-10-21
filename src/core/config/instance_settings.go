package config

import (
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"go.yaml.in/yaml/v3"
	"path/filepath"
	"runtime"
	"strings"
)

// Paths are write-only in the management API. Omitted paths retain the existing
// binding; a new root must explicitly supply its path. Executables are installed
// through deployment, never replaced through a settings request.
type RootSettings struct {
	ID             string                       `json:"id"`
	Name           string                       `json:"name"`
	Path           *string                      `json:"path,omitempty"`
	ImportDefaults *pluginproto.ImportOverrides `json:"importDefaults,omitempty"`
}
type InstanceSettings struct {
	Name           *string                      `json:"name,omitempty"`
	MusicRoots     *[]RootSettings              `json:"musicRoots,omitempty"`
	Watch          *WatchConfig                 `json:"watch,omitempty"`
	ImportDefaults *pluginproto.ImportOverrides `json:"importDefaults,omitempty"`
	FFmpegPath     *string                      `json:"ffmpegPath,omitempty"`
	FFprobePath    *string                      `json:"ffprobePath,omitempty"`
}

func setNode(parent *yaml.Node, name string, value any) error {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return err
	}
	if existing := mappingValue(parent, name); existing != nil {
		head, line, foot := existing.HeadComment, existing.LineComment, existing.FootComment
		*existing = node
		existing.HeadComment, existing.LineComment, existing.FootComment = head, line, foot
	} else {
		parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, &node)
	}
	return nil
}

func applyInstanceSettings(doc *Document, change Change, node *yaml.Node) error {
	s := change.Settings
	if s == nil || change.InstanceID == "" {
		return domain.Fail("INVALID_ARGUMENT", "Instance settings require an instance")
	}
	if s.Name != nil {
		if err := setNode(node, "name", *s.Name); err != nil {
			return err
		}
	}
	if s.ImportDefaults != nil {
		if err := setNode(node, "importDefaults", s.ImportDefaults); err != nil {
			return err
		}
	}
	configNode := mappingValue(node, "config")
	if configNode == nil {
		if err := setNode(node, "config", map[string]any{}); err != nil {
			return err
		}
		configNode = mappingValue(node, "config")
	}
	if s.Watch != nil {
		if err := setNode(configNode, "watch", s.Watch); err != nil {
			return err
		}
	}
	if s.FFmpegPath != nil {
		if err := setNode(configNode, "ffmpegPath", *s.FFmpegPath); err != nil {
			return err
		}
	}
	if s.FFprobePath != nil {
		if err := setNode(configNode, "ffprobePath", *s.FFprobePath); err != nil {
			return err
		}
	}
	if s.MusicRoots != nil {
		// Read the original YAML node to preserve relative paths on untouched roots.
		existing := []MusicRoot{}
		if roots := mappingValue(configNode, "musicRoots"); roots != nil {
			if err := roots.Decode(&existing); err != nil {
				return err
			}
		}
		roots := make([]MusicRoot, 0, len(*s.MusicRoots))
		for _, input := range *s.MusicRoots {
			r := MusicRoot{ID: input.ID, Name: input.Name, ImportDefaults: input.ImportDefaults}
			if input.Path != nil {
				r.Path = *input.Path
			} else {
				for _, old := range existing {
					if old.ID == input.ID {
						r.Path = old.Path
						break
					}
				}
			}
			if r.Path == "" {
				return domain.Fail("INVALID_ARGUMENT", "New music roots require an explicit path")
			}
			for _, old := range existing {
				if old.ID != r.ID {
					continue
				}
				if input.ImportDefaults == nil && old.ImportDefaults != nil {
					r.ImportDefaults = old.ImportDefaults.Clone()
				}
				resolve := func(path string) string {
					if !filepath.IsAbs(path) {
						path = filepath.Join(filepath.Dir(doc.Path), path)
					}
					path = filepath.Clean(path)
					if runtime.GOOS == "windows" {
						path = strings.ToLower(path)
					}
					return path
				}
				if resolve(old.Path) != resolve(r.Path) {
					return domain.Fail("INVALID_ARGUMENT", "Existing music root IDs cannot change directories; use a new root ID")
				}
			}
			roots = append(roots, r)
		}
		if err := setNode(configNode, "musicRoots", roots); err != nil {
			return err
		}
	}
	return nil
}
