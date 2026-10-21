package scraperlocal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore/content"
)

const maxMetadataBytes = 16 << 20
const maxContentBytes = 32 << 20

type OpenResource func(context.Context, enrichment.Resource) (io.ReaderAt, error)
type storedCandidate struct {
	descriptor enrichment.Candidate
	body       []byte
}
type Engine struct {
	mu           sync.Mutex
	request      enrichment.SearchRequest
	open         OpenResource
	searched     bool
	items        map[string]storedCandidate
	result       enrichment.SearchResult
	contentBytes int
}

func NewEngine(request enrichment.SearchRequest, open OpenResource) (*Engine, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if open == nil {
		return nil, fmt.Errorf("INVALID_ARGUMENT: resource reader required")
	}
	request.Fields = append([]string(nil), request.Fields...)
	request.Resources = append([]enrichment.Resource(nil), request.Resources...)
	return &Engine{request: request, open: open, items: map[string]storedCandidate{}, result: enrichment.SearchResult{Candidates: []enrichment.Candidate{}, Issues: []enrichment.Issue{}}}, nil
}

func (e *Engine) Search(ctx context.Context) (found enrichment.SearchResult, searchErr error) {
	ctx = pluginlog.Context(ctx, PluginID, e.request.Context.InstanceID)
	finish := pluginlog.Operation(ctx, "scraper.search")
	defer func() { finish(searchErr) }()
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return enrichment.SearchResult{}, err
	}
	if e.searched {
		pluginlog.Logger(ctx).Log(ctx, pluginlog.LevelTrace, "Scraper cached result reused", "candidateCount", len(e.result.Candidates))
		return cloneResult(e.result), nil
	}
	completed := false
	defer func() {
		if !completed {
			e.items = map[string]storedCandidate{}
			e.result = enrichment.SearchResult{Candidates: []enrichment.Candidate{}, Issues: []enrichment.Issue{}}
			e.contentBytes = 0
		}
	}()
	for _, resource := range e.request.Resources {
		if err := ctx.Err(); err != nil {
			return enrichment.SearchResult{}, err
		}
		reader, err := e.open(ctx, resource)
		if err != nil || reader == nil {
			e.issue(resource, "RESOURCE_CHANGED")
			continue
		}
		if resource.Kind == "audio" {
			err = e.embedded(ctx, resource, reader)
		} else {
			limit := content.MaxLyricsBytes
			if resource.Kind == "artwork" {
				limit = content.MaxArtworkBytes
			}
			var body []byte
			body, err = readBounded(ctx, reader, resource.SizeBytes, 0, resource.SizeBytes, limit)
			if err == nil {
				err = e.add(resource, resource.Kind, "und", "sidecar", resource.Match, body, nil)
			} else {
				_ = e.add(resource, resource.Kind, "und", "sidecar", resource.Match, nil, err)
			}
		}
		if err != nil {
			e.issue(resource, "UNSUPPORTED")
		}
	}
	if err := ctx.Err(); err != nil {
		return enrichment.SearchResult{}, err
	}
	e.searched, completed = true, true
	pluginlog.Logger(ctx).DebugContext(ctx, "Scraper search completed", "resourceCount", len(e.request.Resources), "candidateCount", len(e.result.Candidates), "issueCount", len(e.result.Issues))
	return cloneResult(e.result), nil
}

func (e *Engine) issue(resource enrichment.Resource, code string) {
	if len(e.result.Issues) < enrichment.MaxResources {
		e.result.Issues = append(e.result.Issues, enrichment.Issue{ResourceRef: resource.ResourceRef, Code: code})
	}
}

func (e *Engine) add(resource enrichment.Resource, field, language, origin, basis string, body []byte, parseErr error) error {
	if !slices.Contains(e.request.Fields, field) {
		return nil
	}
	if len(e.items) >= enrichment.MaxCandidates {
		return fmt.Errorf("RATE_LIMITED: candidate limit exceeded")
	}
	var data content.Validated
	if parseErr == nil {
		data, parseErr = content.Validate(field, body)
	}
	if parseErr == nil && len(data.Bytes) > maxContentBytes-e.contentBytes {
		return fmt.Errorf("RATE_LIMITED: preview content limit exceeded")
	}
	id, err := randomRef()
	if err != nil {
		return err
	}
	if language == "" {
		language = "und"
	}
	candidate := enrichment.Candidate{CandidateID: id, Target: e.request.Target, Field: field, Language: language, Origin: origin, ResourceRef: resource.ResourceRef, ResourceVersion: resource.Version, Basis: basis, Status: "available"}
	if parseErr != nil {
		candidate.Status = "unavailable"
		code := "UNSUPPORTED"
		candidate.FailureCode = &code
	} else {
		candidate.Format, candidate.MIME, candidate.SHA256, candidate.SizeBytes = data.Format, data.MIME, data.SHA256, int64(len(data.Bytes))
		e.contentBytes += len(data.Bytes)
	}
	e.items[id] = storedCandidate{descriptor: candidate, body: data.Bytes}
	e.result.Candidates = append(e.result.Candidates, candidate)
	return nil
}

func (e *Engine) Fetch(ctx context.Context, candidateID string, output io.Writer) (enrichment.Candidate, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return enrichment.Candidate{}, err
	}
	item, ok := e.items[candidateID]
	if !ok || !e.searched || output == nil {
		return enrichment.Candidate{}, fmt.Errorf("NOT_FOUND: scraper candidate unavailable")
	}
	if item.descriptor.Status != "available" {
		return enrichment.Candidate{}, fmt.Errorf("UNSUPPORTED: candidate content unavailable")
	}
	for offset := 0; offset < len(item.body); {
		if err := ctx.Err(); err != nil {
			return enrichment.Candidate{}, err
		}
		end := min(offset+64*1024, len(item.body))
		n, err := output.Write(item.body[offset:end])
		if err != nil {
			return enrichment.Candidate{}, err
		}
		if n != end-offset {
			return enrichment.Candidate{}, io.ErrShortWrite
		}
		offset = end
	}
	return item.descriptor, nil
}

func cloneResult(value enrichment.SearchResult) enrichment.SearchResult {
	value.Candidates = append([]enrichment.Candidate{}, value.Candidates...)
	for i := range value.Candidates {
		if value.Candidates[i].FailureCode != nil {
			code := *value.Candidates[i].FailureCode
			value.Candidates[i].FailureCode = &code
		}
	}
	value.Issues = append([]enrichment.Issue{}, value.Issues...)
	return value
}

func randomRef() (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("TEMPORARY_FAILURE: resource identity unavailable")
	}
	return hex.EncodeToString(value[:]), nil
}

func readBounded(ctx context.Context, reader io.ReaderAt, size, offset, length int64, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if offset < 0 || length < 0 || offset > size || length > size-offset || length > int64(limit) {
		return nil, fmt.Errorf("UNSUPPORTED: metadata length outside limits")
	}
	data := make([]byte, int(length))
	n, err := reader.ReadAt(data, offset)
	if n != len(data) || (err != nil && err != io.EOF) {
		return nil, fmt.Errorf("RESOURCE_CHANGED: metadata read failed")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}
