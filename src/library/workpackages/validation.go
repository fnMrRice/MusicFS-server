package workpackages

import (
	"encoding/json"
	"math"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

// ValidPath 只接受规范的斜杠相对路径，根节点单独允许点号。
func ValidPath(value string, allowRoot bool) bool {
	return len(value) <= MaxPathBytes && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0 &&
		(value == "." && allowRoot || pluginproto.ValidScanPath(value))
}

func validText(value string, limit int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit && strings.IndexFunc(value, unicode.IsControl) < 0
}

// ValidatePublication 不信任调用方提供的节点 ID；目录父子关系由规范路径确定。
func ValidatePublication(input Publication) error {
	p := input.Package
	invalid := func() error { return domain.Fail("INVALID_ARGUMENT", "Invalid work package publication") }
	if !works.ValidID(p.WorkID) || !works.ValidID(p.ProviderID) || !validText(p.RootID, 512) || strings.ContainsAny(p.RootID, "/\\") ||
		!ValidPath(p.RelativePath, true) || !validText(p.Name, 256) || !validText(p.RootIdentity, 512) ||
		!validText(p.ConfigRevision, 512) || !validText(p.Signature, 512) ||
		input.ExpectedWorkRevision < 1 || input.ExpectedPackageRevision < 0 || input.ExpectedPackageRevision == math.MaxInt64 ||
		len(p.Nodes) < 1 || len(p.Nodes) > MaxNodes || len(input.Observations) > MaxNodes || (p.ID != "" && !works.ValidID(p.ID)) {
		return invalid()
	}
	nodes := make(map[string]Node, len(p.Nodes))
	audio := make(map[string]Node)
	for _, node := range p.Nodes {
		if !ValidPath(node.Path, true) || !ValidPath(node.SourcePath, true) || !validText(node.Name, 256) || node.Order < 0 || node.Order >= MaxNodes || node.SizeBytes < 0 ||
			(node.Path != "." && (strings.Count(node.Path, "/")+1 > MaxDepth || path.Base(node.Path) != node.Name)) ||
			node.SourcePath != path.Join(p.RelativePath, node.Path) {
			return invalid()
		}
		if _, exists := nodes[node.Path]; exists {
			return invalid()
		}
		nodes[node.Path] = node
		switch node.Kind {
		case "directory":
			if node.SizeBytes != 0 || node.MIME != "" || node.ExternalID != "" || node.AttachmentKind != "" || (node.Version != "" && !validText(node.Version, 512)) {
				return invalid()
			}
		case "audio":
			if !works.ValidID(node.ExternalID) || !validText(node.Version, 512) || !validText(node.MIME, 256) || node.AttachmentKind != "" {
				return invalid()
			}
			if _, exists := audio[node.ExternalID]; exists {
				return invalid()
			}
			audio[node.ExternalID] = node
		case "attachment":
			if node.ExternalID != "" || !validText(node.Version, 512) || !validText(node.MIME, 256) ||
				(node.AttachmentKind != "image" && node.AttachmentKind != "text" && node.AttachmentKind != "download") {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	if nodes["."].Kind != "directory" {
		return invalid()
	}
	for name := range nodes {
		if name != "." && nodes[path.Dir(name)].Kind != "directory" {
			return invalid()
		}
	}
	if len(audio) != len(input.Observations) {
		return invalid()
	}
	seen := make(map[string]bool, len(audio))
	for _, observation := range input.Observations {
		node, exists := audio[observation.ExternalID]
		if !exists || seen[observation.ExternalID] || observation.RootID != p.RootID || observation.RelativePath != node.SourcePath ||
			observation.ObservedVersion != node.Version || observation.SizeBytes != node.SizeBytes || observation.MIME != node.MIME ||
			!validText(observation.DisplayName, 256) {
			return invalid()
		}
		seen[observation.ExternalID] = true
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > MaxDocumentBytes {
		return domain.Fail("INVALID_ARGUMENT", "Work package metadata exceeds its limit")
	}
	return nil
}
