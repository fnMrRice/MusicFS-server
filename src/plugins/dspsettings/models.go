// Package dspsettings 管理每用户音效链；设置只影响之后创建的播放会话。
package dspsettings

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

const StorageID = "musicfs.dsp.pipeline"
const GlobalStorageID = "musicfs.dsp.global"
const GlobalOwner = "__musicfs_system__"
const SchemaRevision = "dsp-settings-v1"
const DefaultBufferFrames = 4096
const MinBufferFrames = 64
const MaxBufferFrames = 16384

type Options struct {
	Chain        []audio.NodeSpec `json:"chain"`
	OutputLayout string           `json:"outputLayout"`
	BufferFrames int              `json:"bufferFrames"`
}

type Config struct {
	Defaults          Options
	Plugins           []dspworker.Config
	Available         bool
	DisableEffects    bool
	DisableChannelMix bool
	UnavailableReason string
	OutputLayouts     []string
	Validate          func(context.Context, Options) error
}

type Page struct {
	Scope             string             `json:"scope"`
	UsesGlobal        bool               `json:"usesGlobal"`
	Revision          int64              `json:"revision"`
	SchemaRevision    string             `json:"schemaRevision"`
	Settings          Options            `json:"settings"`
	Defaults          Options            `json:"defaults"`
	Catalog           []audio.Descriptor `json:"catalog"`
	MaxNodes          int                `json:"maxNodes"`
	Available         bool               `json:"available"`
	UnavailableReason string             `json:"unavailableReason,omitempty"`
	OutputLayouts     []string           `json:"outputLayouts"`
}

type Request struct {
	InheritGlobal    bool     `json:"inheritGlobal,omitempty"`
	ExpectedRevision *int64   `json:"expectedRevision"`
	SchemaRevision   string   `json:"schemaRevision"`
	Settings         *Options `json:"settings"`
}

type Step struct {
	Name   string       `json:"name"`
	Input  audio.Format `json:"input"`
	Output audio.Output `json:"output"`
}

type Preview struct {
	Input  audio.Format `json:"input"`
	Output audio.Output `json:"output"`
	Nodes  []Step       `json:"nodes"`
}
