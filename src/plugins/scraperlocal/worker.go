package scraperlocal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type worker struct {
	mu     sync.Mutex
	hello  helloRequest
	engine *Engine
	client *http.Client
	ctx    context.Context
}

func RunWorker(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	w := &worker{ctx: ctx}
	peer := pluginproto.NewPeer(ctx, input, output, "w-", w.handle)
	<-peer.Done()
	cancel()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.client != nil {
		w.client.CloseIdleConnections()
	}
	return peer.Err()
}

func (w *worker) handle(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if method == "host.hello" {
		var hello helloRequest
		if decode(raw, &hello) != nil || w.hello.Nonce != "" || hello.ProtocolVersion != pluginproto.ProtocolVersion || hello.ConfigRevision != "1" || hello.InstanceID != hello.Context.InstanceID {
			return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid scraper handshake")
		}
		u, err := validateBaseURL(hello.BaseURL, hello.Nonce)
		if err != nil {
			return nil, err
		}
		probe := enrichment.SearchRequest{Context: hello.Context, Target: hello.Target, Fields: []string{"lyrics"}, Languages: []string{}, Resources: []enrichment.Resource{}}
		if err = probe.Validate(); err != nil {
			return nil, err
		}
		w.hello = hello
		w.client = localClient(u.Host)
		return pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{PluginID: PluginID, PluginVersion: "0.1.0", ProtocolVersion: pluginproto.ProtocolVersion, Roles: []string{"scraper"}, Capabilities: []string{"scrape.search", "scrape.fetch", "lyrics", "artwork"}, Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: enrichment.MaxCandidates}}, nil
	}
	if w.hello.Nonce == "" {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: scraper handshake required")
	}
	switch method {
	case "scrape.search":
		var request enrichment.SearchRequest
		if decode(raw, &request) != nil || request.Context != w.hello.Context || request.Target != w.hello.Target {
			return nil, fmt.Errorf("ACCESS_DENIED: scraper search scope mismatch")
		}
		if w.engine != nil {
			return nil, fmt.Errorf("CONFLICT: scraper search is already bound")
		}
		engine, err := NewEngine(request, func(ctx context.Context, r enrichment.Resource) (io.ReaderAt, error) {
			return &remoteReader{ctx: ctx, client: w.client, url: w.hello.BaseURL + "/input/" + r.ResourceRef, size: r.SizeBytes}, nil
		})
		if err != nil {
			return nil, err
		}
		w.engine = engine
		return engine.Search(ctx)
	case "scrape.fetch":
		var request enrichment.FetchRequest
		if decode(raw, &request) != nil || request.Context != w.hello.Context || request.Target != w.hello.Target || !enrichment.ValidRef(request.CandidateID) || !enrichment.ValidRef(request.OutputRef) || w.engine == nil {
			return nil, fmt.Errorf("ACCESS_DENIED: scraper fetch scope mismatch")
		}
		var body bytes.Buffer
		candidate, err := w.engine.Fetch(ctx, request.CandidateID, &body)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.hello.BaseURL+"/output/"+request.OutputRef, &body)
		if err != nil {
			return nil, fmt.Errorf("ACCESS_DENIED: invalid scraper output")
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		response, err := w.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("TEMPORARY_FAILURE: scraper output delivery failed")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			return nil, fmt.Errorf("TEMPORARY_FAILURE: scraper output rejected")
		}
		return enrichment.FetchResult{Candidate: candidate, ContentRef: request.OutputRef}, nil
	default:
		return nil, pluginproto.Failure("UNSUPPORTED", "Unknown scraper method")
	}
}
