package plugincontrol

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

type Hooks struct {
	Start          func(context.Context, config.Plugin, config.Instance) error
	Stop           func(context.Context) error
	Ready          func() bool
	Capabilities   func() []string
	PluginIdentity func() *pluginproto.PluginIdentity
	PluginVersion  func() string
	DSPVersion     func(string) string
	DisplayCount   func() *plugindisplay.Count
	InstanceName   func(context.Context, string) string
	AccountName    func(context.Context, string) string
}

type Instance struct {
	PluginID          string   `json:"pluginId"`
	InstanceID        string   `json:"instanceId"`
	Name              string   `json:"name"`
	OwnerUserID       string   `json:"ownerUserId,omitempty"`
	AccountName       string   `json:"accountName,omitempty"`
	Role              string   `json:"role"`
	Capabilities      []string `json:"capabilities"`
	ConfiguredEnabled bool     `json:"configuredEnabled"`
	EffectiveEnabled  bool     `json:"effectiveEnabled"`
	AcceptingNewWork  bool     `json:"acceptingNewWork"`
	RuntimeState      string   `json:"runtimeState"`
	Generation        int64    `json:"generation"`
	ActiveLeases      int      `json:"activeLeases"`
	Reason            *string  `json:"reason"`
}
type Plugin struct {
	PluginID                  string                      `json:"pluginId"`
	Plugin                    *pluginproto.PluginIdentity `json:"plugin"`
	Version                   string                      `json:"version"`
	DisplayCount              *plugindisplay.Count        `json:"displayCount,omitempty"`
	SupportsMultipleInstances bool                        `json:"supportsMultipleInstances"`
	ConfiguredEnabled         bool                        `json:"configuredEnabled"`
	Required                  bool                        `json:"required,omitempty"`
	StateMutable              bool                        `json:"stateMutable"`
	Storage                   *StorageStatus              `json:"storage,omitempty"`
	DSP                       *DSPStatus                  `json:"dsp,omitempty"`
	Instances                 []Instance                  `json:"instances"`
}
type Snapshot struct {
	Revision        string   `json:"revision"`
	AppliedRevision string   `json:"appliedRevision"`
	Writable        bool     `json:"writable"`
	DiskChanged     bool     `json:"diskChanged"`
	RestartRequired bool     `json:"restartRequired"`
	Items           []Plugin `json:"items"`
}
type Operation struct {
	ID              string           `json:"operationId"`
	Owner           string           `json:"-"`
	Revision        string           `json:"revision"`
	PluginID        string           `json:"pluginId"`
	InstanceID      string           `json:"instanceId"`
	Mode            string           `json:"mode"`
	Status          string           `json:"status"`
	RestartRequired bool             `json:"restartRequired"`
	ErrorCode       *string          `json:"errorCode"`
	CreatedAt       time.Time        `json:"createdAt"`
	UpdatedAt       time.Time        `json:"updatedAt"`
	Targets         map[string]int64 `json:"-"`
}
type Repository interface {
	SavePluginOperation(context.Context, Operation) error
	GetPluginOperation(context.Context, string, string) (Operation, error)
	RecoverPluginOperations(context.Context) error
}

type Event struct {
	Revision string `json:"revision"`
	Sequence int64  `json:"sequence"`
}
