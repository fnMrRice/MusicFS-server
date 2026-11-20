package settings

import (
	"context"
	"encoding/json"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"math"
	"unicode/utf8"
)

type Service struct {
	repo      Repository
	providers map[string]Provider
}

func New(repo Repository, providers ...Provider) *Service {
	s := &Service{repo: repo, providers: map[string]Provider{}}
	for _, p := range providers {
		s.providers[p.ID] = p
	}
	return s
}
func Defaults(schema pluginsettings.Schema) Values {
	v := Values{}
	for _, f := range schema.Fields {
		v[f.Key] = f.DefaultValue
	}
	return v
}
func Normalize(schema pluginsettings.Schema, supplied Values) (Values, error) {
	if supplied == nil {
		return nil, domain.Fail("INVALID_ARGUMENT", "Settings values are required")
	}
	values := Defaults(schema)
	fields := map[string]pluginsettings.Field{}
	for _, f := range schema.Fields {
		fields[f.Key] = f
	}
	for k, v := range supplied {
		f, ok := fields[k]
		if !ok {
			return nil, domain.Fail("INVALID_ARGUMENT", "Unknown settings field")
		}
		valid := false
		switch f.Type {
		case "checkbox":
			_, valid = v.(bool)
		case "text":
			str, ok := v.(string)
			valid = ok && utf8.ValidString(str) && utf8.RuneCountInString(str) <= 4096
		case "select":
			str, ok := v.(string)
			if ok {
				for _, o := range f.Options {
					if str == o.Value {
						valid = true
					}
				}
			}
		case "number":
			raw, err := json.Marshal(v)
			var n float64
			valid = err == nil && json.Unmarshal(raw, &n) == nil && v != nil && !math.IsNaN(n) && !math.IsInf(n, 0)
			if valid {
				valid = (f.Min == nil || n >= *f.Min) && (f.Max == nil || n <= *f.Max)
				v = n
			}
		}
		if !valid {
			return nil, domain.Fail("INVALID_ARGUMENT", "Invalid settings field value")
		}
		values[k] = v
	}
	return values, nil
}
func (s *Service) Read(ctx context.Context, owner, id string) (Page, error) {
	p, ok := s.providers[id]
	if !ok {
		return Page{}, domain.Fail("NOT_FOUND", "Plugin settings not found")
	}
	r, err := s.repo.GetPluginSettings(ctx, owner, id)
	if err != nil {
		return Page{}, err
	}
	if r.Values == nil {
		r.Values = Defaults(p.Schema)
	} else {
		r.Values, err = Normalize(p.Schema, r.Values)
		if err != nil {
			return Page{}, err
		}
	}
	if p.Validate != nil {
		if err := p.Validate(r.Values); err != nil {
			return Page{}, err
		}
	}
	job := Job{Status: "idle"}
	if p.Job != nil {
		job, err = p.Job(ctx, owner)
	}
	return Page{PluginID: id, Schema: p.Schema, Record: r, Job: job}, err
}
func (s *Service) Update(ctx context.Context, owner, id string, r Request) (Page, error) {
	p, ok := s.providers[id]
	if !ok {
		return Page{}, domain.Fail("NOT_FOUND", "Plugin settings not found")
	}
	if r.ExpectedRevision == nil || *r.ExpectedRevision < 0 || r.SchemaRevision != p.Schema.Revision {
		return Page{}, domain.Fail("INVALID_ARGUMENT", "Settings revision is required")
	}
	values, err := Normalize(p.Schema, r.Values)
	if err != nil {
		return Page{}, err
	}
	if p.Validate != nil {
		if err := p.Validate(values); err != nil {
			return Page{}, err
		}
	}
	if _, err = s.repo.PutPluginSettings(ctx, owner, id, *r.ExpectedRevision, values); err != nil {
		return Page{}, err
	}
	return s.Read(ctx, owner, id)
}
func (s *Service) Execute(ctx context.Context, owner, id, action, key string) (Job, error) {
	p, ok := s.providers[id]
	if !ok {
		return Job{}, domain.Fail("NOT_FOUND", "Plugin settings not found")
	}
	for _, a := range p.Schema.Actions {
		if a.ID == action {
			if p.Action == nil {
				return Job{}, domain.Fail("UNSUPPORTED", "Plugin action is unavailable")
			}
			return p.Action(ctx, owner, action, key)
		}
	}
	return Job{}, domain.Fail("NOT_FOUND", "Plugin settings action not found")
}
