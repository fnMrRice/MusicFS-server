package pluginproto

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

const ScanPlanVersion = 1
const MaxScanDepth = 64

type ImportOptions struct {
	Recursive       bool     `json:"recursive" yaml:"recursive"`
	MaxDepth        int      `json:"maxDepth" yaml:"maxDepth"`
	ImportLyrics    bool     `json:"importLyrics" yaml:"importLyrics"`
	ImportArtwork   bool     `json:"importArtwork,omitempty" yaml:"importArtwork,omitempty"`
	ExcludePatterns []string `json:"excludePatterns" yaml:"excludePatterns"`
	SkipHidden      bool     `json:"skipHidden,omitempty" yaml:"skipHidden,omitempty"`
	RespectNoMedia  bool     `json:"respectNoMedia,omitempty" yaml:"respectNoMedia,omitempty"`
}

func (o ImportOptions) Validate() error {
	if o.MaxDepth < 0 || o.MaxDepth > MaxScanDepth || !o.Recursive && o.MaxDepth != 0 {
		return fault.Fail("INVALID_ARGUMENT", "Invalid recursive scan depth")
	}
	_, err := CanonicalExclusions(o.ExcludePatterns)
	return err
}

type ScanRoot struct {
	RootID       string `json:"rootId"`
	RootIdentity string `json:"rootIdentity"`
}

type ScanPlan struct {
	PlanVersion    int                      `json:"planVersion"`
	Mode           string                   `json:"mode"`
	PluginID       string                   `json:"pluginId"`
	PluginVersion  string                   `json:"pluginVersion"`
	SchemaRevision string                   `json:"schemaRevision"`
	ConfigRevision string                   `json:"configRevision"`
	Roots          []ScanRoot               `json:"roots"`
	Options        ImportOptions            `json:"options"`
	RootOptions    map[string]ImportOptions `json:"rootOptions,omitempty"`
}

type ScanScope struct {
	RootID          string   `json:"rootId"`
	RootIdentity    string   `json:"rootIdentity"`
	Recursive       bool     `json:"recursive"`
	MaxDepth        int      `json:"maxDepth"`
	ExcludePatterns []string `json:"excludePatterns"`
	SkipHidden      bool     `json:"skipHidden,omitempty"`
	RespectNoMedia  bool     `json:"respectNoMedia,omitempty"`
}

func scanLabel(s string) bool {
	return s != "" && len(s) <= 512 && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n\\/")
}

func (s ScanScope) Validate() error {
	if !scanLabel(s.RootID) || !scanLabel(s.RootIdentity) {
		return fault.Fail("INVALID_ARGUMENT", "Invalid scan root binding")
	}
	return (ImportOptions{Recursive: s.Recursive, MaxDepth: s.MaxDepth, ExcludePatterns: s.ExcludePatterns}).Validate()
}

func (p ScanPlan) Canonical() (ScanPlan, error) {
	if p.PlanVersion != ScanPlanVersion || (p.Mode != "full" && p.Mode != "incremental") || !scanLabel(p.PluginID) || !scanLabel(p.PluginVersion) || !scanLabel(p.SchemaRevision) || !scanLabel(p.ConfigRevision) || len(p.Roots) == 0 || len(p.Roots) > 256 {
		return ScanPlan{}, fault.Fail("INVALID_ARGUMENT", "Invalid scan plan")
	}
	if err := p.Options.Validate(); err != nil {
		return ScanPlan{}, err
	}
	p.Options.ExcludePatterns, _ = CanonicalExclusions(p.Options.ExcludePatterns)
	p.Roots = append([]ScanRoot(nil), p.Roots...)
	rootOptions := make(map[string]ImportOptions, len(p.RootOptions))
	sort.Slice(p.Roots, func(i, j int) bool { return p.Roots[i].RootID < p.Roots[j].RootID })
	for i, root := range p.Roots {
		if !scanLabel(root.RootID) || !scanLabel(root.RootIdentity) || (i > 0 && p.Roots[i-1].RootID == root.RootID) {
			return ScanPlan{}, fault.Fail("INVALID_ARGUMENT", "Invalid or repeated scan root")
		}
		if options, ok := p.RootOptions[root.RootID]; ok {
			if err := options.Validate(); err != nil {
				return ScanPlan{}, err
			}
			options.ExcludePatterns, _ = CanonicalExclusions(options.ExcludePatterns)
			rootOptions[root.RootID] = options
		}
	}
	if len(rootOptions) != len(p.RootOptions) {
		return ScanPlan{}, fault.Fail("INVALID_ARGUMENT", "Import options reference an unselected root")
	}
	p.RootOptions = rootOptions
	return p, nil
}

func (p ScanPlan) Digest() (string, error) {
	canonical, err := p.Canonical()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(append([]byte("musicfs-scan-plan-v1\x00"), raw...))
	return hex.EncodeToString(hash[:]), nil
}

func (p ScanPlan) Scope(rootID string) (ScanScope, error) {
	canonical, err := p.Canonical()
	if err != nil {
		return ScanScope{}, err
	}
	for _, root := range canonical.Roots {
		if root.RootID == rootID {
			options := canonical.Options
			if selected, ok := canonical.RootOptions[rootID]; ok {
				options = selected
			}
			return ScanScope{RootID: root.RootID, RootIdentity: root.RootIdentity, Recursive: options.Recursive, MaxDepth: options.MaxDepth, ExcludePatterns: options.ExcludePatterns, SkipHidden: options.SkipHidden, RespectNoMedia: options.RespectNoMedia}, nil
		}
	}
	return ScanScope{}, fault.Fail("ACCESS_DENIED", "Root is outside the scan plan")
}

// ValidScanPath accepts canonical slash-separated relative file names. The
// protocol never interprets backslashes, drive prefixes, dot segments, or NUL.
func ValidScanPath(p string) bool {
	if p == "." || !fs.ValidPath(p) || strings.ContainsAny(p, "\\\x00") {
		return false
	}
	if len(p) > 1 && p[1] == ':' && ((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z')) {
		return false
	}
	return true
}

func (s ScanScope) Contains(rootID, relativePath string) bool {
	return s.Validate() == nil && rootID == s.RootID && ValidScanPath(relativePath) && s.containsDepth(strings.Count(relativePath, "/")) && !Excludes(s.ExcludePatterns, relativePath) && (!s.SkipHidden || !HasHiddenPathSegment(relativePath))
}

// A zero depth is unlimited only when recursion is enabled. Flat scans still
// cover root files alone, so reconciliation cannot remove unvisited children.
func (s ScanScope) containsDepth(depth int) bool {
	return depth == 0 || s.Recursive && (s.MaxDepth == 0 || depth <= s.MaxDepth)
}

func (p ScanPlan) Contains(rootID, relativePath string) bool {
	s, err := p.Scope(rootID)
	return err == nil && s.Contains(rootID, relativePath)
}
