package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func localCall(ctx context.Context, peer *pluginproto.Peer, mode, instance, method string, raw json.RawMessage) (any, error) {
	if method == "plugin.displayCount" {
		if mode == "old-worker" {
			return nil, pluginproto.Failure("UNSUPPORTED", "Older worker")
		}
		var r pluginproto.DisplayCountRequest
		if json.Unmarshal(raw, &r) != nil || r.InstanceID != instance || len(r.RootIDs) != 2 {
			return nil, pluginproto.Failure("INVALID_ARGUMENT", "Root identities are required")
		}
		return &plugindisplay.Count{Value: 7, Unit: "custom units", LocalizedUnits: map[string]string{"zh": "个自定义单位"}}, nil
	}
	if method == "provider.resolve" {
		var r pluginproto.ResolveRequest
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		operation, key, _ := strings.Cut(r.ExternalID, ":")
		var result pluginproto.StateValue
		err := peer.Call(ctx, "host.database."+operation, pluginproto.StateRequest{Key: key, Value: []byte(`{"recursive":true}`)}, &result)
		return pluginproto.Resource{SizeBytes: result.Revision}, err
	}
	if method != "provider.scan" {
		return nil, pluginproto.Failure("UNSUPPORTED", "Unsupported fixture method")
	}
	var r pluginproto.ScanRequest
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	var entries pluginproto.EntriesPage
	if mode != "missing-receipt" {
		err := peer.Call(ctx, "host.entries", pluginproto.EntriesRequest{GrantID: r.GrantID, RootID: r.RootID, Cursor: r.Cursor, Limit: r.Limit}, &entries)
		if err != nil {
			return nil, err
		}
	}
	page := pluginproto.ScanPage{RootIdentity: entries.RootIdentity, NextCursor: entries.NextCursor, Issues: entries.Issues,
		Complete: entries.NextCursor == "" && len(entries.Issues) == 0, PlanDigest: r.PlanDigest, ScanID: r.ScanID, Generation: r.Generation}
	for _, e := range entries.Items {
		page.Items = append(page.Items, pluginproto.Observation{RootID: e.RootID, RelativePath: e.RelativePath, ObservedVersion: e.ObservedVersion, SizeBytes: e.SizeBytes})
	}
	switch mode {
	case "hidden-path":
		page.Items[0].RelativePath = "private/hidden.wav"
	case "wrong-root":
		page.Items[0].RootID = "outside"
	case "new-version":
		page.Items[0].ObservedVersion = "invented"
	case "missing-entry":
		page.Items = nil
	case "extra-entry":
		page.Items = append(page.Items, page.Items[0])
	case "wrong-cursor":
		page.NextCursor = "invented"
	case "wrong-identity":
		page.RootIdentity = "other"
	case "partial":
		page.Complete = false
	}
	// Host evidence must ignore even an explicitly forged wire field.
	body, err := json.Marshal(page)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err = json.Unmarshal(body, &value); err != nil {
		return nil, err
	}
	value["excludedPaths"] = []string{"."}
	return value, nil
}
