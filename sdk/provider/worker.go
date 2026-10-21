// Package provider serves online clients over the plugin control protocol.
package provider

import (
	"context"
	"encoding/json"
	"io"
	"runtime"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

type worker struct {
	mu     sync.Mutex
	plugin Plugin
	hello  pluginproto.OnlineHello
	client Client
	slots  chan struct{}
}

// Run serves one plugin instance until its context or protocol channel closes.
func Run(ctx context.Context, plugin Plugin, input io.ReadCloser, output io.WriteCloser) error {
	plugin.Descriptor = plugin.Descriptor.Clone()
	if err := plugin.Validate(); err != nil {
		return err
	}
	w := &worker{plugin: plugin, slots: make(chan struct{}, 2)}
	peer := pluginproto.NewPeer(ctx, input, output, "w-", w.handle)
	peer.Wait()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.client != nil {
		w.client.Close()
	}
	return peer.Err()
}
func (w *worker) handle(ctx context.Context, method string, raw json.RawMessage) (result any, err error) {
	defer func() {
		if err != nil {
			err = rpcError(err)
		}
	}()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if method == "host.hello" {
		return w.handshake(raw)
	}
	w.mu.Lock()
	hello, client := w.hello, w.client
	w.mu.Unlock()
	if client == nil {
		return nil, pluginproto.Failure("PROTOCOL_MISMATCH", "Online handshake is required")
	}
	priority := false
	required := methodCapability(method)
	var scope pluginproto.Context
	var run func(context.Context) (any, error)
	switch method {
	case "provider.online.account":
		var r pluginproto.OnlineAccount
		if err = pluginproto.DecodeOnline(raw, &r); err != nil {
			return nil, err
		}
		scope = r.Context
		priority = r.Request.Action == "signalCancel" || r.Request.Action == "logout"
		run = func(ctx context.Context) (any, error) { return client.Account(ctx, r.Request) }
	case "provider.online.describe":
		var r pluginproto.OnlineScope
		if err = pluginproto.DecodeOnline(raw, &r); err != nil {
			return nil, err
		}
		scope = r.Context
		run = func(context.Context) (any, error) { return w.plugin.Descriptor, nil }
	case "provider.online.search":
		var r pluginproto.OnlineSearch
		if err = pluginproto.DecodeOnline(raw, &r); err != nil {
			return nil, err
		}
		scope = r.Context
		kind := r.Kind
		if kind == "" {
			kind = "tracks"
		}
		required = "search." + kind
		run = func(ctx context.Context) (any, error) {
			return client.Search(ctx, online.SearchRequest{Kind: r.Kind, Query: r.Query, Limit: r.Limit, Cursor: r.Cursor})
		}
	case "provider.online.metadata", "provider.online.playlist", "provider.online.resolve":
		var r pluginproto.OnlineReference
		if err = pluginproto.DecodeOnline(raw, &r); err != nil {
			return nil, err
		}
		scope = r.Context
		run = func(ctx context.Context) (any, error) {
			switch method {
			case "provider.online.metadata":
				return client.Metadata(ctx, r.ExternalID)
			case "provider.online.playlist":
				return client.ObservePlaylist(ctx, r.ExternalID)
			default:
				return nil, client.Resolve(ctx, r.ExternalID)
			}
		}
	case "provider.online.album", "provider.online.artist":
		var r pluginproto.OnlineReference
		if err = pluginproto.DecodeOnline(raw, &r); err != nil {
			return nil, err
		}
		scope = r.Context
		run = func(ctx context.Context) (any, error) {
			if method == "provider.online.album" {
				return client.Album(ctx, r.ExternalID)
			}
			return client.Artist(ctx, r.ExternalID)
		}
	case "provider.online.albumTracks", "provider.online.artistAlbums", "provider.online.artistTracks":
		var r pluginproto.OnlineEntityPage
		if err = pluginproto.DecodeOnline(raw, &r); err != nil {
			return nil, err
		}
		scope = r.Context
		run = func(ctx context.Context) (any, error) {
			switch method {
			case "provider.online.albumTracks":
				return client.AlbumTracks(ctx, r.EntityPageRequest)
			case "provider.online.artistAlbums":
				return client.ArtistAlbums(ctx, r.EntityPageRequest)
			default:
				return client.ArtistTracks(ctx, r.EntityPageRequest)
			}
		}
	case "provider.online.metadataBatch":
		var r pluginproto.OnlineTrackBatch
		if err = pluginproto.DecodeOnline(raw, &r); err != nil {
			return nil, err
		}
		scope = r.Context
		run = func(ctx context.Context) (any, error) { return client.MetadataBatch(ctx, r.ExternalIDs) }
	case "provider.online.playlistTracks":
		var r pluginproto.OnlinePlaylistTracks
		if err = pluginproto.DecodeOnline(raw, &r); err != nil {
			return nil, err
		}
		scope = r.Context
		run = func(ctx context.Context) (any, error) { return client.PlaylistTracks(ctx, r.PlaylistTracksRequest) }
	case "provider.online.playlists":
		var r pluginproto.OnlinePlaylists
		if err = pluginproto.DecodeOnline(raw, &r); err != nil {
			return nil, err
		}
		scope = r.Context
		run = func(ctx context.Context) (any, error) { return client.Playlists(ctx, r.PlaylistsRequest) }
	case "provider.online.recommendations":
		var r pluginproto.OnlineRecommendations
		if err = pluginproto.DecodeOnline(raw, &r); err != nil {
			return nil, err
		}
		scope = r.Context
		run = func(ctx context.Context) (any, error) { return client.PublicRecommendations(ctx, r.Limit) }
	default:
		return nil, pluginproto.Failure("UNSUPPORTED", "Unknown online provider method")
	}
	if err = hello.ValidateScope(scope); err != nil {
		return nil, err
	}
	if !priority {
		select {
		case w.slots <- struct{}{}:
			defer func() { <-w.slots }()
		default:
			return nil, pluginproto.Failure("RATE_LIMITED", "Online worker is busy")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(scope.TimeoutMs)*time.Millisecond)
	defer cancel()
	if required != "" && !w.plugin.supports(required) {
		// Preserve login-first behavior without making supported requests read
		// account state twice. Implementations authorize their enabled methods.
		account, accountErr := client.Account(ctx, online.AccountRequest{Action: "state"})
		if accountErr != nil {
			return nil, accountErr
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if account.View.State != "signedIn" || account.View.AccountID == "" || account.View.Revision == "" {
			return nil, pluginproto.Failure("PLATFORM_AUTH_REQUIRED", "Sign in to the music platform")
		}
		return nil, pluginproto.Failure("UNSUPPORTED", "Online capability is not enabled by this plugin")
	}
	value, err := run(ctx)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	result = pluginproto.OnlineResult[any]{Context: scope, Value: value}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > pluginproto.MaxMessageBytes-4096 {
		return nil, pluginproto.Failure("RATE_LIMITED", "Online metadata exceeds the control message limit")
	}
	return result, nil
}

func (w *worker) handshake(raw json.RawMessage) (pluginproto.Ready, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var hello pluginproto.OnlineHello
	if pluginproto.DecodeOnline(raw, &hello) != nil || hello.Validate() != nil || w.client != nil {
		return pluginproto.Ready{}, pluginproto.Failure("PROTOCOL_MISMATCH", "Invalid online handshake")
	}
	client, err := w.plugin.NewClient(online.Scope{UserID: hello.UserID, InstanceID: hello.InstanceID}, time.Duration(hello.TimeoutMs)*time.Millisecond)
	if err != nil {
		return pluginproto.Ready{}, err
	}
	if client == nil {
		return pluginproto.Ready{}, pluginproto.Failure("TEMPORARY_FAILURE", "Online plugin factory returned no client")
	}
	w.hello, w.client = hello, client
	manifest := pluginproto.Manifest{PluginID: w.plugin.Descriptor.Plugin.ID, Plugin: w.plugin.Descriptor.Plugin, PluginVersion: w.plugin.Version, ProtocolVersion: pluginproto.ProtocolVersion, SupportsMultipleInstances: true, Roles: []string{"provider"}, Capabilities: w.plugin.capabilities(), Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: online.MaxPageItems}
	return pluginproto.Ready{Manifest: manifest, InstanceID: hello.InstanceID, Nonce: hello.Nonce}, nil
}
