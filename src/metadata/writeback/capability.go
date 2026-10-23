package writeback

import "github.com/fnMrRice/MusicFS-server/sdk/tagformat"

// Capability is a host-only root assessment. A positive result does not waive
// per-file protection, permissions, format or attribute checks at execution.
// Policy.Enabled is independent: a capable root is never implicitly enabled.
type Capability struct {
	Available   bool     `json:"available"`
	Formats     []string `json:"formats"`
	Fields      []string `json:"fields"`
	Replacement string   `json:"replacement"`
}

// InspectCapability does not create or modify any file. On Windows it opens
// the actual protected directory chain, verifies local NTFS and requests the
// directory access rights needed to create and rename entries.
func InspectCapability(root Root, format tagformat.Format) (Capability, error) {
	if format == nil {
		return Capability{}, unsupported("Tag format plugin is unavailable")
	}
	descriptor := format.Descriptor()
	if err := descriptor.Validate(); err != nil {
		return Capability{}, err
	}
	if err := inspectCapability(root); err != nil {
		return Capability{}, err
	}
	return Capability{Available: true, Formats: append([]string{}, descriptor.Formats...), Fields: append([]string{}, descriptor.Fields...), Replacement: "recoverableReplace"}, nil
}
