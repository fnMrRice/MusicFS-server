package pluginhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type scanBinding struct {
	scanID     string
	generation int64
	digest     string
	scope      pluginproto.ScanScope
}

func (b scanBinding) equal(other scanBinding) bool {
	return b.scanID == other.scanID && b.generation == other.generation && b.digest == other.digest && b.scope.Equal(other.scope)
}

type scanGrant struct {
	binding scanBinding
	cursor  string
	limit   int
	ctx     context.Context
	receipt *scanReceipt
}

func (w *Worker) authorizeScan(ctx context.Context, r *pluginproto.ScanRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	digest, err := hex.DecodeString(r.PlanDigest)
	if err != nil || len(digest) != 32 || strings.ToLower(r.PlanDigest) != r.PlanDigest || r.ScanID == "" || r.Generation < 1 || r.Scope.RootID != r.RootID || r.Limit < 1 || r.Limit > pluginproto.MaxPageItems {
		return "", fmt.Errorf("ACCESS_DENIED: invalid scan authorization")
	}
	if err := r.Scope.Validate(); err != nil {
		return "", err
	}
	r.Scope.ExcludePatterns, _ = pluginproto.CanonicalExclusions(r.Scope.ExcludePatterns)
	allowed := false
	for _, root := range w.Access.Roots() {
		allowed = allowed || root.ID == r.RootID && root.ExpectedIdentity == r.Scope.RootIdentity
	}
	if !allowed {
		return "", fmt.Errorf("ACCESS_DENIED: scan root is not authorized")
	}
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	r.GrantID = hex.EncodeToString(random[:])
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.scanGrants) >= 16 {
		return "", fmt.Errorf("RATE_LIMITED: too many active scans")
	}
	w.scanGrants[r.GrantID] = scanGrant{binding: scanBinding{scanID: r.ScanID, generation: r.Generation, digest: r.PlanDigest, scope: r.Scope}, cursor: r.Cursor, limit: r.Limit, ctx: ctx}
	return r.GrantID, nil
}

func (w *Worker) scanAuthorization(r pluginproto.EntriesRequest) (scanGrant, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	g, ok := w.scanGrants[r.GrantID]
	if !ok || g.binding.scope.RootID != r.RootID || g.cursor != r.Cursor || g.limit != r.Limit {
		return scanGrant{}, fmt.Errorf("ACCESS_DENIED: scan page is outside its authorized task")
	}
	if err := g.ctx.Err(); err != nil {
		return scanGrant{}, err
	}
	return g, nil
}
