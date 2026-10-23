"""Apply metadata plugin boundary updates to the current working tree."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def update(name, transform):
    path = ROOT / name
    old = path.read_text(encoding="utf-8")
    new = transform(old)
    if new != old:
        path.write_text(new, encoding="utf-8", newline="\n")

def replace(name, old, new):
    update(name, lambda value: value.replace(old, new))

replace("plugins/scraper/local/host.go", "type Input struct {\n\tResource enrichment.Resource\n\tReader   io.ReaderAt\n\tValidate func() error\n}", "type Input = enrichment.Input")
replace("plugins/scraper/local/host.go", "(opened *Session, openErr error)", "(opened enrichment.Session, openErr error)")
replace("plugins/scraper/local/host.go", "func (h *Host) PluginVersion() string {", "func (*Host) Descriptor() enrichment.Descriptor { return Descriptor() }\n\n// Descriptor 返回本地插件支持的字段与查询能力。\nfunc Descriptor() enrichment.Descriptor {\n\treturn enrichment.Descriptor{PluginID: PluginID, Fields: []string{\"lyrics\", \"artwork\"}}\n}\n\nfunc (h *Host) PluginVersion() string {")
for name in ["src/library/scrape_source.go", "src/library/scrape_decode.go", "src/metadata/enrichmentworker/preview.go", "src/metadata/enrichmentworker/document.go", "src/metadata/enrichmentworker/service.go"]:
    replace(name, '\t"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"\n', '')
    replace(name, "scraperlocal.Input", "enrichment.Input")
    replace(name, "*scraperlocal.Session", "enrichment.Session")
replace("src/metadata/enrichmentworker/service.go", "Host            *scraperlocal.Host", "Host            enrichment.Host\n\tDescriptor enrichment.Descriptor")
replace("src/metadata/enrichmentworker/service.go", '\t\tif r.ID == ""', '\t\tif r.Host != nil { r.Descriptor = r.Host.Descriptor() }\n\t\tif r.Descriptor.PluginID != "" {\n\t\t\tif err := r.Descriptor.Validate(); err != nil { return nil, err }\n\t\t}\n\t\tr.Descriptor.Fields = slices.Clone(r.Descriptor.Fields)\n\t\tif r.ID == ""')
replace("src/metadata/enrichmentworker/service.go", 'PluginID: scraperlocal.PluginID, Availability: state, Fields: []string{"lyrics", "artwork"}, QueryCorrectionsSupported: false', 'PluginID: r.Descriptor.PluginID, Availability: state, Fields: slices.Clone(r.Descriptor.Fields), QueryCorrectionsSupported: r.Descriptor.QueryCorrectionsSupported')
replace("src/metadata/enrichmentworker/service.go", '\tif request.Query != nil {\n\t\tif request.Query.Title != "" || len(request.Query.Artists) > 0 {\n\t\t\treturn enrichment.Job{}, false, domain.Fail("UNSUPPORTED", "Local scrapers do not support query corrections")\n\t\t}\n\t\trequest.Query = nil\n\t}', '\tif request.Query != nil && request.Query.Title == "" && len(request.Query.Artists) == 0 { request.Query = nil }')
replace("src/metadata/enrichmentworker/service.go", '!slices.Contains(request.ExcludeInstanceIDs, r.ID) {', '!slices.Contains(request.ExcludeInstanceIDs, r.ID) && r.Descriptor.Supports(request.Fields, request.Query) {')
replace("src/metadata/enrichmentworker/service.go", '\t\tr, _ := s.Runtime(id)\n\t\tif r.Host == nil', '\t\tr, _ := s.Runtime(id)\n\t\tif !r.Descriptor.Supports(request.Fields, request.Query) { return enrichment.Job{}, false, domain.Fail("UNSUPPORTED", "Selected scraper does not support the requested fields or query corrections") }\n\t\tif r.Host == nil')
replace("src/metadata/enrichmentworker/document.go", 'func (d *documentTrace) attempt(id string)', 'func (d *documentTrace) attempt(id, pluginID string)')
replace("src/metadata/enrichmentworker/document.go", 'PluginID: scraperlocal.PluginID', 'PluginID: pluginID')
replace("src/metadata/enrichmentworker/execute.go", 'trace.attempt(id)', 'trace.attempt(id, r.Descriptor.PluginID)')
replace("src/metadata/enrichmentworker/execute.go", 'session.Search(ctx, w.Request.Fields, w.Request.Languages, nil)', 'session.Search(ctx, w.Request.Fields, w.Request.Languages, w.Request.Query)')
replace("src/metadata/enrichmentworker/runtime.go", '\t"github.com/fnMrRice/MusicFS-server/src/core/domain"', '\t"github.com/fnMrRice/MusicFS-server/src/core/domain"\n\t"slices"')
replace("src/metadata/enrichmentworker/runtime.go", '\tr.Epoch = r.Gate.Epoch()', '\tif r.Host != nil { r.Descriptor = r.Host.Descriptor() } else if r.Descriptor.PluginID == "" { r.Descriptor = old.Descriptor }\n\tif err := r.Descriptor.Validate(); err != nil { return err }\n\tif old.Descriptor.PluginID != "" && old.Descriptor.PluginID != r.Descriptor.PluginID { return domain.Fail("CONFLICT", "Scraper plugin identity changed") }\n\tr.Descriptor.Fields = slices.Clone(r.Descriptor.Fields)\n\tr.Epoch = r.Gate.Epoch()')
replace("src/core/config/metadata.go", 'import "errors"', 'import ("errors"\n"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"\n)')
replace("src/core/config/metadata.go", 'plugin.PluginID == "musicfs.scraper.local"', 'pluginconfig.Role(plugin.PluginID) == "scraper"')
replace("src/core/config/metadata.go", 'configured local scraper', 'configured scraper')
replace("src/app/bootstrap/enrichment.go", 'Owner: "admin", Enabled:', 'Owner: "admin", Descriptor: scraperlocal.Descriptor(), Enabled:')
replace("src/app/bootstrap/scraper_management.go", 'Capabilities: func() []string { return []string{"lyrics", "artwork"} },', 'Capabilities: func() []string { r, _ := service.Runtime(id); return append([]string{}, r.Descriptor.Fields...) },')

# Move the analyzer-specific identity out of the public fingerprint contract.
for path in list((ROOT / "tests").rglob("*.go")) + list((ROOT / "plugins/analysis").rglob("*.go")) + list((ROOT / "src/metadata").rglob("*.go")):
    update(str(path.relative_to(ROOT)), lambda text: text.replace(".Fpcalc.", ".Analyzer.").replace("Fpcalc:", "Analyzer:").replace("fingerprint.AlgorithmVersion", '"1.6.1"'))
replace("src/metadata/identification/fingerprint/models.go", 'const AlgorithmVersion = "1.6.1"\n', '')
replace("src/metadata/identification/fingerprint/models.go", 'Fpcalc                ToolIdentity `json:"fpcalc"`', 'Analyzer              ToolIdentity `json:"analyzer"`')
replace("src/metadata/identification/fingerprint/detail.go", 'id.Algorithm != "chromaprint" || id.AlgorithmVersion != AlgorithmVersion || id.AlgorithmConfig != "cli-2-raw-v1"', '!validIdentityText(id.Algorithm, 128) || !validIdentityText(id.AlgorithmVersion, 128) || !validIdentityText(id.AlgorithmConfig, 128)')
replace("src/metadata/identification/fingerprint/detail.go", 'id.Decoder.Version == "" || len(id.Decoder.Version) > 512 ||\n\t\tlen(id.Analyzer.Version) > 512 || strings.ContainsAny(id.Analyzer.Version, "\\r\\n") || !strings.HasPrefix(id.Analyzer.Version, "fpcalc version 1.6.1 (") || !strings.HasSuffix(id.Analyzer.Version, ")")', '!validIdentityText(id.Decoder.Version, 512) || !validIdentityText(id.Analyzer.Version, 512)')
replace("src/metadata/identification/fingerprint/detail.go", '// Validate checks the complete pinned algorithm identity without running tools.', '// Validate 验证公共证据身份和资源边界；具体算法与工具版本由插件负责。')
replace("src/metadata/identification/fingerprint/detail.go", 'func validSHA(value string) bool {', 'func validIdentityText(value string, limit int) bool {\n\treturn value != "" && len(value) <= limit && !strings.ContainsAny(value, "\\x00\\r\\n")\n}\n\nfunc validSHA(value string) bool {')
replace("tests/unit/identification/fingerprint_test.go", 'v.AlgorithmVersion = "1.6.0"', 'v.AlgorithmVersion = ""')
replace("tests/unit/identification/fingerprint_test.go", 'v.AlgorithmConfig = "cli-1-raw-v1"', 'v.AlgorithmConfig = ""')
replace("tests/unit/identification/fingerprint_test.go", 'v.Analyzer.Version = "fpcalc version 1.6.0 (test)"', 'v.Analyzer.Version = "invalid\\nversion"')
