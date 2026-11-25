package library

import (
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
)

func providerEvidence(o pluginproto.Observation) ([]identification.Evidence, error) {
	if o.SizeBytes < 0 || o.DurationMs != nil && *o.DurationMs < 0 {
		return nil, identification.Invalid()
	}
	if o.BitrateMode != nil && *o.BitrateMode != "unknown" && *o.BitrateMode != "cbr" && *o.BitrateMode != "vbr" {
		return nil, identification.Invalid()
	}
	if o.BitrateBps != nil && *o.BitrateBps <= 0 {
		return nil, identification.Invalid()
	}
	if o.ProviderSHA512 != nil && !identification.ValidDigest(*o.ProviderSHA512) {
		return nil, identification.Invalid()
	}
	result := []identification.Evidence{}
	now := time.Now().UTC()
	spec := identification.Spec{Kind: "bitrate", Origin: "provider", Algorithm: "provider-metadata", AlgorithmVersion: "1", ConfigRevision: "provider-observation-v1", Range: identification.Range{Unit: "bytes", End: &o.SizeBytes}}
	if o.BitrateBps != nil {
		mode := "unknown"
		if o.BitrateMode != nil {
			mode = *o.BitrateMode
		}
		result = append(result, identification.Evidence{Spec: spec, Result: identification.Result{Status: "reported", BitrateBps: o.BitrateBps, BitrateMode: mode, ComputedAt: &now}})
	}
	if o.ProviderSHA512 != nil {
		spec.Kind, spec.Algorithm = "providerSha512", "sha512"
		result = append(result, identification.Evidence{Spec: spec, Result: identification.Result{Status: "reported", BitrateMode: "unknown", SHA512: o.ProviderSHA512, ComputedAt: &now}})
	}
	return result, nil
}

// EvidenceForObservation also serves the repository boundary used by importers.
// Provider inputs cannot supply the origin or a server verification status.
func EvidenceForObservation(o Observation) ([]identification.Evidence, error) {
	if o.ProbeStatus != "" && o.ProbeStatus != "notAttempted" && o.ProbeStatus != "succeeded" && o.ProbeStatus != "failed" {
		return nil, identification.Invalid()
	}
	if (len(o.Evidence) > 0 || o.AlbumRelease != nil || o.NetEaseID != "" || o.NetEaseIDConflict) && o.ProbeStatus != "succeeded" {
		return nil, identification.Invalid()
	}
	if o.NetEaseID != "" && (o.NetEaseIDConflict || !audiotypes.ValidNetEaseID(o.NetEaseID)) {
		return nil, identification.Invalid()
	}
	if r := o.AlbumRelease; r != nil && (!r.Valid() || r.Namespace != "netease" || o.NetEaseID == "" || o.Title == nil || strings.TrimSpace(*o.Title) == "" || o.Album == nil || strings.TrimSpace(*o.Album) == "" || len(NormalizeArtists(o.Artists)) == 0) {
		return nil, identification.Invalid()
	}
	input := o.Observation
	if o.ProviderObservation != nil {
		input = *o.ProviderObservation
	}
	declared, err := providerEvidence(input)
	if err != nil {
		return nil, err
	}
	for _, e := range o.Evidence {
		if e.Kind != "bitrate" || e.Origin != "probe" && e.Origin != "estimate" {
			return nil, identification.Invalid()
		}
	}
	return append(declared, o.Evidence...), nil
}
