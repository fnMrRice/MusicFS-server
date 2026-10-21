package pluginproto

import "fmt"

const ProtocolVersion = "1.0"
const MaxMessageBytes = 1 << 20
const MaxPageItems = 200

type Manifest struct {
	PluginID                  string          `json:"pluginId"`
	Plugin                    *PluginIdentity `json:"plugin"`
	SupportsMultipleInstances bool            `json:"supportsMultipleInstances"`
	PluginVersion             string          `json:"pluginVersion"`
	ProtocolVersion           string          `json:"protocolVersion"`
	Roles                     []string        `json:"roles"`
	Capabilities              []string        `json:"capabilities"`
	Systems                   []string        `json:"systems"`
	Architectures             []string        `json:"architectures"`
	MaxMessageBytes           int             `json:"maxMessageBytes"`
	MaxPageItems              int             `json:"maxPageItems"`
	Configuration             *Configuration  `json:"configuration"`
}

func (m Manifest) Validate(role string) error {
	if m.PluginID == "" || !ValidPluginVersion(m.PluginVersion) || m.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("PROTOCOL_MISMATCH: invalid plugin identity or protocol version")
	}
	found := false
	for _, r := range m.Roles {
		if r != "provider" && r != "audio" && r != "scraper" {
			return fmt.Errorf("PROTOCOL_MISMATCH: unknown role")
		}
		found = found || r == role
	}
	if !found || m.MaxMessageBytes <= 0 || m.MaxMessageBytes > MaxMessageBytes || m.MaxPageItems <= 0 || m.MaxPageItems > MaxPageItems {
		return fmt.Errorf("PROTOCOL_MISMATCH: invalid role or resource limits")
	}
	if err := m.Plugin.Validate(m.PluginID); err != nil {
		return err
	}
	if m.Configuration != nil {
		if role != "provider" {
			return fmt.Errorf("PROTOCOL_MISMATCH: import configuration requires provider role")
		}
		return m.Configuration.Validate()
	}
	return nil
}

type Context struct {
	InstanceID string `json:"instanceId"`
	UserID     string `json:"userId"`
	TaskID     string `json:"taskId"`
	AccountRef string `json:"accountRef,omitempty"`
	TimeoutMs  int64  `json:"timeoutMs"`
}

type Hello struct {
	ProtocolVersion string `json:"protocolVersion"`
	InstanceID      string `json:"instanceId"`
	TaskID          string `json:"taskId,omitempty"`
	Nonce           string `json:"nonce"`
	ConfigRevision  string `json:"configRevision"`
}

type Ready struct {
	Manifest   Manifest `json:"manifest"`
	InstanceID string   `json:"instanceId"`
	Nonce      string   `json:"nonce"`
}
