package bootstrap

import (
	"context"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
	"github.com/fnMrRice/MusicFS-server/src/writebackhost"
)

func writeBackPolicy(c config.Config, runtimes []library.Runtime, _ bool) writeback.Policy {
	policy := writeback.Policy{Enabled: c.WriteBack.Enabled, MaxFileBytes: c.WriteBack.MaxFileBytes}
	for i := range runtimes {
		r := &runtimes[i]
		r.WriteBack = map[string]library.RootWriteBack{}
		roots := map[string]sourceaccess.RootConfig{}
		if r.Worker != nil {
			for _, root := range r.Worker.Access.Roots() {
				roots[root.ID] = root
			}
		}
		for id := range r.RootNames {
			cap := library.RootWriteBack{Formats: []string{}, Fields: []string{}}
			var granted []string
			for _, g := range c.WriteBack.Roots {
				if g.InstanceID == r.ID && g.RootID == id {
					granted = append([]string{}, g.Fields...)
					break
				}
			}
			cap.Enabled = policy.Enabled && len(granted) > 0
			// Bind preconfigured grants even when this provider starts disabled.
			// This is read-only and never creates a worker or changes source files.
			if _, ok := roots[id]; !ok && len(granted) > 0 {
				for _, plugin := range c.Plugins {
					for _, instance := range plugin.Instances {
						if instance.InstanceID != r.ID {
							continue
						}
						for _, root := range instance.Config.MusicRoots {
							if root.ID != id {
								continue
							}
							if access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: id, Path: root.Path}}); err == nil {
								roots[id] = access.Roots()[0]
							}
						}
					}
				}
			}
			if root, ok := roots[id]; ok {
				cap.BoundPath, cap.BoundIdentity = root.Path, root.ExpectedIdentity
				grant := writeback.Root{Owner: r.Owner, InstanceID: r.ID, RootID: id, Path: root.Path, Identity: root.ExpectedIdentity, Fields: granted}
				if len(granted) > 0 {
					policy.Roots = append(policy.Roots, grant)
				}
				if actual, err := writeback.InspectCapability(grant); err == nil {
					cap.Available = actual.Available
					cap.Formats = actual.Formats
					cap.Replacement = actual.Replacement
					cap.Fields = append([]string{}, granted...)
				}
			}
			r.WriteBack[id] = cap
		}
	}
	return policy
}

func configureWriteBack(ctx context.Context, c config.Config, store *sqlite.Store, lib *library.Service, content *mediastore.ContentService, runtimes []library.Runtime, policy writeback.Policy, probe library.Probe, probeSessions ...library.ProbeSession) (*writebackhost.Service, func(), error) {
	journal, err := writeback.OpenJournal(filepath.Join(c.DataRoot, "writeback"))
	if err != nil {
		return nil, nil, err
	}
	settings := writebackhost.Config{Policy: policy, AllowOverwrite: c.WriteBack.AllowOverwrite, Probe: probe}
	if len(probeSessions) > 0 {
		settings.ProbeSession = probeSessions[0]
	}
	service, err := writebackhost.New(store, lib, content, journal, runtimes, settings)
	if err != nil {
		journal.Close()
		return nil, nil, err
	}
	return service, func() { service.Close(); journal.Close() }, nil
}
