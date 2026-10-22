package dspsettings_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
)

func settingsConfig() dspsettings.Config {
	return dspsettings.Config{Available: true, OutputLayouts: []string{"stereo", "5.1", "7.1"}, Defaults: dspsettings.Options{
		Chain: []audio.NodeSpec{}, OutputLayout: "stereo", BufferFrames: 4096}}
}

func request(revision int64, options dspsettings.Options) dspsettings.Request {
	return dspsettings.Request{ExpectedRevision: &revision, SchemaRevision: dspsettings.SchemaRevision, Settings: &options}
}

func TestGlobalDefaultsPersonalOverrideInheritanceAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.db")
	store, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	service := dspsettings.New(store, settingsConfig)
	initial, err := service.Read(t.Context(), "listener")
	if err != nil || !initial.UsesGlobal || initial.Revision != 0 {
		t.Fatal(initial, err)
	}
	global := settingsConfig().Defaults
	global.BufferFrames = 128
	global.Chain = []audio.NodeSpec{{Name: "equalizer", Version: 1, Params: map[string]float64{"gainDB": 3}}}
	page, err := service.UpdateGlobal(t.Context(), request(0, global), "global-save-one")
	if err != nil || page.Revision != 1 || page.Scope != "global" {
		t.Fatal(page, err)
	}
	inherited, err := dspsettings.Load(t.Context(), store, "listener")
	if err != nil || inherited == nil || inherited.BufferFrames != 128 {
		t.Fatal(inherited, err)
	}
	personal := settingsConfig().Defaults
	personal.BufferFrames = 16384
	page, err = service.Update(t.Context(), "listener", request(0, personal), "personal-save-one")
	if err != nil || page.UsesGlobal || page.Settings.BufferFrames != 16384 {
		t.Fatal(page, err)
	}
	retry, err := service.Update(t.Context(), "listener", request(0, personal), "personal-save-one")
	if err != nil || retry.Revision != page.Revision {
		t.Fatal("idempotent retry changed revision", retry, err)
	}
	if _, err = service.Update(t.Context(), "listener", request(0, personal), "different-operation"); err == nil {
		t.Fatal("stale revision accepted")
	}
	global.BufferFrames = 256
	if _, err = service.UpdateGlobal(t.Context(), request(1, global), "global-save-two"); err != nil {
		t.Fatal(err)
	}
	page, err = service.Read(t.Context(), "listener")
	if err != nil || page.Settings.BufferFrames != 16384 || page.Defaults.BufferFrames != 256 {
		t.Fatal(page, err)
	}
	other, err := service.Read(t.Context(), "other")
	if err != nil || !other.UsesGlobal || other.Settings.BufferFrames != 256 {
		t.Fatal(other, err)
	}
	revision := int64(1)
	inherit := dspsettings.Request{ExpectedRevision: &revision, SchemaRevision: dspsettings.SchemaRevision, InheritGlobal: true}
	if page, err = service.Update(t.Context(), "listener", inherit, "inherit-global-one"); err != nil || !page.UsesGlobal {
		t.Fatal(page, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := dspsettings.Load(t.Context(), store, "listener")
	if err != nil || loaded.BufferFrames != 256 || len(loaded.Chain) != 1 {
		t.Fatal(loaded, err)
	}
	loaded.Chain[0].Params["gainDB"] = 99
	again, err := dspsettings.Load(t.Context(), store, "listener")
	if err != nil || again.Chain[0].Params["gainDB"] != 3 {
		t.Fatal("loaded state shares mutable parameters", again, err)
	}
}

func TestDSPSettingsValidationIsAtomicAndDescribesIntermediateFormats(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := dspsettings.New(store, settingsConfig)
	for _, frames := range []int{0, 63, 16385} {
		options := settingsConfig().Defaults
		options.BufferFrames = frames
		if _, err := service.Update(t.Context(), "listener", request(0, options), "invalid-buffer-save"); err == nil {
			t.Fatal("invalid buffer accepted")
		}
	}
	options := settingsConfig().Defaults
	options.Chain = []audio.NodeSpec{{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 1}}}
	if _, err := service.Validate(t.Context(), options); err == nil {
		t.Fatal("output layout mismatch accepted")
	}
	options.Chain = append(options.Chain, audio.NodeSpec{Name: "equalizer", Version: 1}, audio.NodeSpec{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 2}})
	preview, err := service.Validate(t.Context(), options)
	if err != nil || len(preview.Nodes) != 3 || preview.Nodes[0].Output.Format.Channels != 1 || preview.Nodes[1].Input.Channels != 1 || preview.Output.Format.Channels != 2 {
		t.Fatal(preview, err)
	}
	if _, err := service.Update(t.Context(), "listener", request(0, options), "valid-channel-chain"); err != nil {
		t.Fatal(err)
	}
	options.Chain[1].Params = map[string]float64{"gainDB": 100}
	if _, err := service.Update(t.Context(), "listener", request(1, options), "invalid-parameter-save"); err == nil {
		t.Fatal("invalid gain accepted")
	}
	current, err := service.Read(t.Context(), "listener")
	if err != nil || current.Revision != 1 {
		t.Fatal("invalid save modified state", current, err)
	}
	for _, raw := range []string{`{"chain":[{"name":"gain","version":1,"params":{"gainDB":null}}],"outputLayout":"stereo","bufferFrames":64}`, `{"chain":[],"outputLayout":"stereo","bufferFrames":64,"command":"run"}`} {
		var decoded dspsettings.Options
		if json.Unmarshal([]byte(raw), &decoded) == nil {
			t.Fatal("invalid JSON options accepted")
		}
	}
	unavailable := dspsettings.New(store, func() dspsettings.Config { cfg := settingsConfig(); cfg.Available = false; return cfg })
	_, err = unavailable.Update(t.Context(), "listener", request(1, settingsConfig().Defaults), "unavailable-save")
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != "UNSUPPORTED" {
		t.Fatal("unavailable processor save accepted", err)
	}
}

func TestDSPSettingsEmptyParametersRetryAfterPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.db")
	store, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	service := dspsettings.New(store, settingsConfig)
	options := settingsConfig().Defaults
	options.Chain = []audio.NodeSpec{{Name: "bypass", Version: 1, Params: map[string]float64{}}}
	input := request(0, options)
	if _, err := service.Update(t.Context(), "listener", input, "personal-empty-params"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateGlobal(t.Context(), input, "global-empty-params"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = dspsettings.New(store, settingsConfig)
	for _, save := range []func() (dspsettings.Page, error){
		func() (dspsettings.Page, error) {
			return service.Update(t.Context(), "listener", input, "personal-empty-params")
		},
		func() (dspsettings.Page, error) {
			return service.UpdateGlobal(t.Context(), input, "global-empty-params")
		},
	} {
		page, err := save()
		if err != nil || page.Revision != 1 || len(page.Settings.Chain) != 1 {
			t.Fatal("retry of an unchanged empty parameter map did not replay", page, err)
		}
	}
}
