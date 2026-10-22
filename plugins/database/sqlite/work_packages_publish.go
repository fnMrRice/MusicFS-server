package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
)

func (s *Store) PublishWorkPackage(ctx context.Context, owner string, input workpackages.Publication, preview, key string) (workpackages.Package, error) {
	var empty workpackages.Package
	route, digest, err := packageOperation(owner, input.Package.WorkID, preview, key)
	if err != nil {
		return empty, err
	}
	if err = workpackages.ValidatePublication(input); err != nil {
		return empty, err
	}
	if input.Package.Owner != "" && input.Package.Owner != owner {
		return empty, domain.Fail("ACCESS_DENIED", "Work package owner does not match")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	raw, found, err := contentIdempotency(ctx, tx, owner, route, key, digest)
	if err != nil {
		return empty, err
	}
	if found {
		return decodeWorkPackage(raw, owner)
	}
	work, err := readWork(ctx, tx, owner, input.Package.WorkID)
	if err != nil {
		return empty, err
	}
	if work.Revision != input.ExpectedWorkRevision {
		return empty, domain.Fail("CONFLICT", "Work revision changed before package import")
	}
	var active int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM provider_scan_leases WHERE owner=? AND provider=? AND active_job_id IS NOT NULL", owner, input.Package.ProviderID).Scan(&active); err != nil {
		return empty, err
	}
	if active != 0 {
		return empty, domain.Fail("CONFLICT", "Provider scan is active; retry package import after it completes")
	}
	previous, err := previousWorkPackage(ctx, tx, owner, input)
	if err != nil {
		return empty, err
	}
	if previous.ID == "" {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_packages WHERE owner=? AND work_id=?", owner, input.Package.WorkID).Scan(&count); err != nil {
			return empty, err
		}
		if count >= workpackages.MaxPackages {
			return empty, domain.Fail("RATE_LIMITED", "Work package limit exceeded")
		}
	}
	p := input.Package
	p.Owner, p.ID, p.Revision, p.ImportedAt = owner, previous.ID, previous.Revision+1, time.Now().UTC()
	if p.ID == "" {
		p.ID = domain.NewID()
	}
	p.Nodes = assignPackageNodes(p.Nodes, previous.Nodes)
	tracks := make(map[string][2]string, len(input.Observations))
	for _, observation := range input.Observations {
		track, err := publishPackageTrack(ctx, tx, owner, p, observation)
		if err != nil {
			return empty, err
		}
		tracks[observation.ExternalID] = [2]string{track.ID, track.AssetID}
	}
	for i := range p.Nodes {
		node := &p.Nodes[i]
		node.SourceTrackID, node.AssetID, node.Track = "", "", nil
		if node.Kind == "audio" {
			binding := tracks[node.ExternalID]
			node.SourceTrackID, node.AssetID = binding[0], binding[1]
		}
	}
	raw, err = encodeWorkPackage(p)
	if err != nil {
		return empty, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO work_packages(id,owner,work_id,provider_id,root_id,relative_path,revision,document)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,document=excluded.document`,
		p.ID, owner, p.WorkID, p.ProviderID, p.RootID, p.RelativePath, p.Revision, raw)
	if err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE library_revision SET revision=revision+1"); err != nil {
		return empty, err
	}
	if err = saveContentIdempotency(ctx, tx, owner, route, key, digest, raw); err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return p, nil
}

func previousWorkPackage(ctx context.Context, tx *sql.Tx, owner string, input workpackages.Publication) (workpackages.Package, error) {
	p := input.Package
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM work_packages WHERE owner=? AND provider_id=? AND root_id=? AND relative_path=?", owner, p.ProviderID, p.RootID, p.RelativePath).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		if input.ExpectedPackageRevision != 0 || p.ID != "" {
			return workpackages.Package{}, domain.Fail("CONFLICT", "Work package location changed or no longer exists")
		}
		return workpackages.Package{}, nil
	}
	if err != nil {
		return workpackages.Package{}, err
	}
	previous, err := decodeWorkPackage(raw, owner)
	if err != nil {
		return previous, err
	}
	if previous.WorkID != p.WorkID || previous.Revision != input.ExpectedPackageRevision || (p.ID != "" && p.ID != previous.ID) {
		return previous, domain.Fail("CONFLICT", "Work package location or revision changed")
	}
	return previous, nil
}

func assignPackageNodes(input, previous []workpackages.Node) []workpackages.Node {
	nodes := append([]workpackages.Node{}, input...)
	byPath, byExternal := map[string]workpackages.Node{}, map[string]workpackages.Node{}
	for _, node := range previous {
		byPath[node.Path] = node
		if node.Kind == "audio" {
			byExternal[node.ExternalID] = node
		}
	}
	used := map[string]bool{}
	for i := range nodes {
		nodes[i].ID = ""
		if old, ok := byExternal[nodes[i].ExternalID]; nodes[i].Kind == "audio" && ok {
			nodes[i].ID, used[old.ID] = old.ID, true
		}
	}
	ids := make(map[string]string, len(nodes))
	for i := range nodes {
		node := &nodes[i]
		if node.ID == "" {
			old, ok := byPath[node.Path]
			if ok && old.Kind == node.Kind && !used[old.ID] {
				node.ID = old.ID
			} else {
				node.ID = domain.NewID()
			}
			used[node.ID] = true
		}
		ids[node.Path] = node.ID
	}
	for i := range nodes {
		nodes[i].ParentID = ""
		if nodes[i].Path != "." {
			nodes[i].ParentID = ids[path.Dir(nodes[i].Path)]
		}
	}
	return nodes
}
