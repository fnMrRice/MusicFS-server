package dspsettings

import (
	"context"
	"encoding/json"
	"reflect"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspchain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

type Service struct {
	repo    settings.Repository
	current func() Config
}

func New(repo settings.Repository, current func() Config) *Service {
	return &Service{repo: repo, current: current}
}

func clone(options Options) Options {
	options.Chain = dspchain.Clone(options.Chain)
	if options.Chain == nil {
		options.Chain = []audio.NodeSpec{}
	}
	// JSON 会省略空参数，统一表示以便保存后的相同请求仍可重放。
	for i := range options.Chain {
		if len(options.Chain[i].Params) == 0 {
			options.Chain[i].Params = nil
		}
	}
	return options
}

// Load 按个人、全局顺序解析；两者未保存时返回 nil 并沿用启动配置。
// 显式继承不会复制全局快照，因此管理员后续修改能作用于下一次播放。
func Load(ctx context.Context, repository any, owner string) (*Options, error) {
	repo, ok := repository.(settings.Repository)
	if !ok {
		return nil, nil
	}
	record, err := repo.GetPluginSettings(ctx, owner, StorageID)
	if err != nil {
		return nil, err
	}
	options, err := decode(record)
	if err != nil || options != nil {
		return options, err
	}
	global, err := repo.GetPluginSettings(ctx, GlobalOwner, GlobalStorageID)
	if err != nil {
		return nil, err
	}
	return decode(global)
}

func decode(record settings.Record) (*Options, error) {
	if record.Values == nil {
		return nil, nil
	}
	if record.Values["schemaRevision"] != SchemaRevision {
		return nil, domain.Fail("INVALID_ARGUMENT", "Stored DSP settings have an unsupported schema")
	}
	if record.Values["inheritGlobal"] == true {
		return nil, nil
	}
	if record.Values["settings"] == nil {
		return nil, domain.Fail("INVALID_ARGUMENT", "Stored DSP settings are invalid")
	}
	raw, err := json.Marshal(record.Values["settings"])
	var options Options
	if err != nil || json.Unmarshal(raw, &options) != nil {
		return nil, domain.Fail("INVALID_ARGUMENT", "Stored DSP settings are invalid")
	}
	if err := validateShape(options); err != nil {
		return nil, err
	}
	copy := clone(options)
	return &copy, nil
}

func (s *Service) Read(ctx context.Context, owner string) (Page, error) {
	record, err := s.repo.GetPluginSettings(ctx, owner, StorageID)
	if err != nil {
		return Page{}, err
	}
	cfg, err := s.personalConfig(ctx)
	if err != nil {
		return Page{}, err
	}
	return s.page(record, cfg, "personal")
}

func (s *Service) personalConfig(ctx context.Context) (Config, error) {
	cfg := s.current()
	global, err := s.repo.GetPluginSettings(ctx, GlobalOwner, GlobalStorageID)
	if err != nil {
		return Config{}, err
	}
	options, err := decode(global)
	if err != nil {
		return Config{}, err
	}
	if options != nil {
		cfg.Defaults = clone(*options)
	}
	return cfg, nil
}

func (s *Service) ReadGlobal(ctx context.Context) (Page, error) {
	record, err := s.repo.GetPluginSettings(ctx, GlobalOwner, GlobalStorageID)
	if err != nil {
		return Page{}, err
	}
	return s.page(record, s.current(), "global")
}

func (s *Service) page(record settings.Record, cfg Config, scope string) (Page, error) {
	options, err := decode(record)
	if err != nil {
		return Page{}, err
	}
	defaults := clone(cfg.Defaults)
	if defaults.OutputLayout == "" {
		defaults.OutputLayout = "stereo"
	}
	if defaults.BufferFrames == 0 {
		defaults.BufferFrames = DefaultBufferFrames
	}
	selected := clone(defaults)
	if options != nil {
		selected = clone(*options)
	}
	layouts := append([]string{}, cfg.OutputLayouts...)
	return Page{Scope: scope, UsesGlobal: scope == "personal" && options == nil, Revision: record.Revision, SchemaRevision: SchemaRevision, Settings: selected,
		Defaults: defaults, Catalog: catalog(cfg), MaxNodes: audio.MaxChainNodes - 1,
		Available: cfg.Available, UnavailableReason: cfg.UnavailableReason, OutputLayouts: layouts}, nil
}

func (s *Service) Update(ctx context.Context, owner string, request Request, key string) (Page, error) {
	cfg, err := s.personalConfig(ctx)
	if err != nil {
		return Page{}, err
	}
	return s.update(ctx, owner, StorageID, "personal", cfg, request, key)
}

func (s *Service) UpdateGlobal(ctx context.Context, request Request, key string) (Page, error) {
	if request.InheritGlobal {
		return Page{}, domain.Fail("INVALID_ARGUMENT", "Global DSP cannot inherit itself")
	}
	return s.update(ctx, GlobalOwner, GlobalStorageID, "global", s.current(), request, key)
}

func (s *Service) update(ctx context.Context, owner, id, scope string, cfg Config, request Request, key string) (Page, error) {
	if request.ExpectedRevision == nil || *request.ExpectedRevision < 0 || request.SchemaRevision != SchemaRevision || (request.Settings == nil) != request.InheritGlobal {
		return Page{}, domain.Fail("INVALID_ARGUMENT", "DSP settings and revision are required")
	}
	if len(key) < 8 || len(key) > 128 || !utf8.ValidString(key) {
		return Page{}, domain.Fail("INVALID_ARGUMENT", "Idempotency-Key must contain 8 to 128 characters")
	}
	if !cfg.Available {
		return Page{}, domain.Fail("UNSUPPORTED", "Audio processor is unavailable")
	}
	current, err := s.repo.GetPluginSettings(ctx, owner, id)
	if err != nil {
		return Page{}, err
	}
	previous, err := decode(current)
	if err != nil {
		return Page{}, err
	}
	// 只重放最后一次已提交的相同请求，不把其他标签页的新修订覆盖掉。
	if current.Values["operationKey"] == key {
		same := request.InheritGlobal && current.Values["inheritGlobal"] == true
		if request.Settings != nil && previous != nil {
			same = reflect.DeepEqual(clone(*previous), clone(*request.Settings))
		}
		if current.Revision == *request.ExpectedRevision+1 && same {
			return s.page(current, cfg, scope)
		}
		return Page{}, domain.Fail("CONFLICT", "DSP operation key was already used")
	}
	if current.Revision != *request.ExpectedRevision {
		return Page{}, domain.Fail("CONFLICT", "DSP settings changed; reload before saving")
	}
	selected := cfg.Defaults
	if request.Settings != nil {
		selected = *request.Settings
	} else if cfg.DisableEffects {
		// 继承旧全局配置时只校验仍会执行的输出布局，不重新激活历史效果。
		selected.Chain = []audio.NodeSpec{}
	}
	if _, err := validate(ctx, cfg, selected); err != nil {
		return Page{}, err
	}
	values := settings.Values{"schemaRevision": SchemaRevision, "operationKey": key, "inheritGlobal": request.InheritGlobal}
	if request.Settings != nil {
		values["settings"] = clone(*request.Settings)
	}
	record, err := s.repo.PutPluginSettings(ctx, owner, id, current.Revision, values)
	if err != nil {
		return Page{}, err
	}
	return s.page(record, cfg, scope)
}

func (s *Service) Validate(ctx context.Context, options Options) (Preview, error) {
	return validate(ctx, s.current(), options)
}
