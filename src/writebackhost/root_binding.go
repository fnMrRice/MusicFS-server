package writebackhost

import (
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
)

// A policy is tied to the startup directory identity. Reusing a root ID after
// a settings update cannot make a queued write operate on the previous path or
// expand that grant to the replacement directory. Recovery retains its barrier
// until the original binding is restored or explicitly reviewed after restart.
func (s *Service) boundRoot(owner, instance, id string, access *sourceaccess.Access) (writeback.Root, error) {
	grant, err := s.grant(owner, instance, id, nil)
	if err != nil {
		return grant, err
	}
	if access == nil {
		return grant, domain.Fail("SOURCE_OFFLINE", "Writeback provider is offline")
	}
	for _, root := range access.Roots() {
		if root.ID == id && root.Path == grant.Path && root.ExpectedIdentity == grant.Identity {
			return grant, nil
		}
	}
	return grant, domain.Fail("ACCESS_DENIED", "Writeback root configuration changed; review grants and restart")
}
