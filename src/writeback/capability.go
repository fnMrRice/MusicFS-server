package writeback

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
func InspectCapability(root Root) (Capability, error) {
	if err := inspectCapability(root); err != nil {
		return Capability{}, err
	}
	return Capability{Available: true, Formats: []string{"flac"}, Fields: []string{"title", "artists", "album", "lyrics", "artwork"}, Replacement: "recoverableReplace"}, nil
}
