package scraperlocal

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"reflect"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore/content"
)

func (s *Session) Search(ctx context.Context, fields, languages []string, query *enrichment.Query) (enrichment.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return enrichment.SearchResult{}, err
	}
	if s.result != nil {
		return enrichment.SearchResult{}, fmt.Errorf("CONFLICT: scraper search already completed")
	}
	if err := s.channel.Validate(); err != nil {
		return enrichment.SearchResult{}, err
	}
	request := enrichment.SearchRequest{Context: s.scope, Target: s.target, Fields: fields, Languages: append([]string{}, languages...), Query: query, Resources: s.resources}
	// 仅授权本次搜索所选字段的附件资源。
	request.Resources = nil
	for _, r := range s.resources {
		if r.Kind == "audio" || contains(fields, r.Kind) {
			request.Resources = append(request.Resources, r)
		}
	}
	if request.Resources == nil {
		request.Resources = []enrichment.Resource{}
	}
	if err := request.Validate(); err != nil {
		return enrichment.SearchResult{}, err
	}
	var result enrichment.SearchResult
	if err := s.peer.Call(ctx, "scrape.search", request, &result); err != nil {
		return enrichment.SearchResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return enrichment.SearchResult{}, err
	}
	if err := s.validateResult(result, request.Resources, fields); err != nil {
		s.cancel()
		return enrichment.SearchResult{}, err
	}
	if err := s.channel.Validate(); err != nil {
		return enrichment.SearchResult{}, err
	}
	s.result = &result
	return cloneResult(result), nil
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func (s *Session) validateResult(result enrichment.SearchResult, resources []enrichment.Resource, fields []string) error {
	if result.Candidates == nil || result.Issues == nil || len(result.Candidates) > enrichment.MaxCandidates || len(result.Issues) > enrichment.MaxResources {
		return fmt.Errorf("PROTOCOL_MISMATCH: invalid scraper result limits")
	}
	refs := map[string]enrichment.Resource{}
	for _, r := range resources {
		refs[r.ResourceRef] = r
	}
	ids := map[string]bool{}
	var total int64
	for _, candidate := range result.Candidates {
		r, ok := refs[candidate.ResourceRef]
		if !ok || candidate.ResourceVersion != r.Version || candidate.Target != s.target || !contains(fields, candidate.Field) || !enrichment.ValidRef(candidate.CandidateID) || ids[candidate.CandidateID] || len(candidate.Language) > 35 || len(candidate.Basis) > 64 {
			return fmt.Errorf("PROTOCOL_MISMATCH: candidate scope mismatch")
		}
		ids[candidate.CandidateID] = true
		if r.Kind == "audio" {
			if candidate.Origin != "embedded" {
				return fmt.Errorf("PROTOCOL_MISMATCH: invalid embedded provenance")
			}
		} else {
			origin := "sidecar"
			if r.Kind == "artwork" && r.Match == "containerEmbedded" {
				origin = "container"
			}
			if candidate.Origin != origin || candidate.Field != r.Kind || candidate.Basis != r.Match {
				return fmt.Errorf("PROTOCOL_MISMATCH: invalid attachment provenance")
			}
		}
		if candidate.Status == "unavailable" {
			if candidate.FailureCode == nil || *candidate.FailureCode != "UNSUPPORTED" || candidate.SizeBytes != 0 || candidate.SHA256 != "" || candidate.Format != "" || candidate.MIME != "" {
				return fmt.Errorf("PROTOCOL_MISMATCH: invalid unavailable candidate")
			}
			continue
		}
		limit := int64(content.MaxLyricsBytes)
		if candidate.Field == "artwork" {
			limit = content.MaxArtworkBytes
		}
		if candidate.Status != "available" || candidate.FailureCode != nil || candidate.SizeBytes <= 0 || candidate.SizeBytes > limit || len(candidate.SHA256) != 64 {
			return fmt.Errorf("PROTOCOL_MISMATCH: invalid available candidate")
		}
		hash, err := hex.DecodeString(candidate.SHA256)
		validFormat := candidate.Field == "lyrics" && (candidate.Format == "lrc" || candidate.Format == "text") && candidate.MIME == "text/plain; charset=utf-8" || candidate.Field == "artwork" && (candidate.Format == "png" && candidate.MIME == "image/png" || candidate.Format == "jpeg" && candidate.MIME == "image/jpeg")
		total += candidate.SizeBytes
		if err != nil || hex.EncodeToString(hash) != candidate.SHA256 || !validFormat || total > maxContentBytes {
			return fmt.Errorf("PROTOCOL_MISMATCH: invalid candidate content descriptor")
		}
	}
	for _, issue := range result.Issues {
		if _, ok := refs[issue.ResourceRef]; !ok || (issue.Code != "UNSUPPORTED" && issue.Code != "RESOURCE_CHANGED") {
			return fmt.Errorf("PROTOCOL_MISMATCH: invalid scraper issue")
		}
	}
	return nil
}

func (s *Session) Fetch(ctx context.Context, candidateID string, output io.Writer) (enrichment.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var selected enrichment.Candidate
	if err := ctx.Err(); err != nil {
		return selected, err
	}
	if output == nil || s.result == nil {
		return selected, fmt.Errorf("NOT_FOUND: preview unavailable")
	}
	for _, candidate := range s.result.Candidates {
		if candidate.CandidateID == candidateID {
			selected = candidate
			break
		}
	}
	if selected.CandidateID == "" {
		return selected, fmt.Errorf("NOT_FOUND: candidate is outside this scraper session")
	}
	if selected.Status != "available" {
		return selected, fmt.Errorf("UNSUPPORTED: candidate content unavailable")
	}
	if err := s.channel.Validate(); err != nil {
		return enrichment.Candidate{}, err
	}
	ref, err := randomRef()
	if err != nil {
		return enrichment.Candidate{}, err
	}
	var body bytes.Buffer
	grant := &outputGrant{writer: &body, size: selected.SizeBytes, done: make(chan error, 1)}
	s.channel.mu.Lock()
	s.channel.outputs[ref] = grant
	s.channel.mu.Unlock()
	defer func() { s.channel.mu.Lock(); delete(s.channel.outputs, ref); s.channel.mu.Unlock() }()
	var result enrichment.FetchResult
	err = s.peer.Call(ctx, "scrape.fetch", enrichment.FetchRequest{Context: s.scope, Target: s.target, CandidateID: candidateID, OutputRef: ref}, &result)
	if ctx.Err() != nil {
		return enrichment.Candidate{}, ctx.Err()
	}
	if err != nil {
		return enrichment.Candidate{}, err
	}
	if result.ContentRef != ref || !reflect.DeepEqual(result.Candidate, selected) {
		return enrichment.Candidate{}, fmt.Errorf("PROTOCOL_MISMATCH: scraper fetch identity changed")
	}
	select {
	case err = <-grant.done:
	case <-ctx.Done():
		err = ctx.Err()
	case <-s.ctx.Done():
		err = s.ctx.Err()
	}
	if err != nil {
		return enrichment.Candidate{}, err
	}
	validated, err := content.Validate(selected.Field, body.Bytes())
	if err != nil || validated.SHA256 != selected.SHA256 || int64(len(validated.Bytes)) != selected.SizeBytes || validated.Format != selected.Format || validated.MIME != selected.MIME {
		return enrichment.Candidate{}, fmt.Errorf("PROTOCOL_MISMATCH: scraper content validation failed")
	}
	if err = s.channel.Validate(); err != nil {
		return enrichment.Candidate{}, err
	}
	if err = ctx.Err(); err != nil {
		return enrichment.Candidate{}, err
	}
	if _, err = io.Copy(output, bytes.NewReader(validated.Bytes)); err != nil {
		return enrichment.Candidate{}, err
	}
	if err = ctx.Err(); err != nil {
		return enrichment.Candidate{}, err
	}
	if err = s.channel.Validate(); err != nil {
		return enrichment.Candidate{}, err
	}
	return selected, nil
}
