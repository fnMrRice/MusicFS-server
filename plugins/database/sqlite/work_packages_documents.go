package sqlite

import (
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
)

// 私有绑定只保存在数据库文档中，不通过公开 Package 的 JSON 字段泄漏。
type packageDocument struct {
	workpackages.Package
	RootIdentity   string                `json:"rootIdentity"`
	ConfigRevision string                `json:"configRevision"`
	Signature      string                `json:"signature"`
	Nodes          []packageNodeDocument `json:"nodes"`
}

type packageNodeDocument struct {
	workpackages.Node
	Version    string `json:"version"`
	SourcePath string `json:"sourcePath"`
	ExternalID string `json:"externalId"`
}

func encodeWorkPackage(p workpackages.Package) ([]byte, error) {
	doc := packageDocument{Package: p, RootIdentity: p.RootIdentity, ConfigRevision: p.ConfigRevision, Signature: p.Signature, Nodes: make([]packageNodeDocument, 0, len(p.Nodes))}
	for _, node := range p.Nodes {
		node.Track = nil
		doc.Nodes = append(doc.Nodes, packageNodeDocument{Node: node, Version: node.Version, SourcePath: node.SourcePath, ExternalID: node.ExternalID})
	}
	raw, err := json.Marshal(doc)
	if err == nil && len(raw) > workpackages.MaxDocumentBytes {
		return nil, domain.Fail("INVALID_ARGUMENT", "Work package metadata exceeds its limit")
	}
	return raw, err
}

func decodeWorkPackage(raw []byte, owner string) (workpackages.Package, error) {
	var doc packageDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return workpackages.Package{}, err
	}
	p := doc.Package
	p.Owner, p.RootIdentity, p.ConfigRevision, p.Signature = owner, doc.RootIdentity, doc.ConfigRevision, doc.Signature
	p.Nodes = make([]workpackages.Node, 0, len(doc.Nodes))
	for _, stored := range doc.Nodes {
		node := stored.Node
		node.Version, node.SourcePath, node.ExternalID = stored.Version, stored.SourcePath, stored.ExternalID
		node.Track = nil
		p.Nodes = append(p.Nodes, node)
	}
	return p, nil
}
