package online

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ReferenceRule 由插件声明外部身份与官方资料链接的绑定规则；不授权音频访问。
type ReferenceRule struct {
	IDPattern   string `json:"idPattern"`
	URLTemplate string `json:"urlTemplate"`
}

// ResourcePolicy 只约束展示资料。网络请求仍由插件及其受控传输完成。
type ResourcePolicy struct {
	nativePlatform        string
	Tracks                []ReferenceRule `json:"tracks"`
	Playlists             []ReferenceRule `json:"playlists"`
	Albums                []ReferenceRule `json:"albums"`
	Artists               []ReferenceRule `json:"artists"`
	PlaylistEntryPatterns []string        `json:"playlistEntryPatterns"`
	ImageHosts            []string        `json:"imageHosts"`
}

// BindPlatform 只由宿主使用。已握手的插件身份约束其可提供的原生发行命名空间。
func (p ResourcePolicy) BindPlatform(platform string) ResourcePolicy {
	p.nativePlatform = platform
	return p
}

var externalIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.-]{0,127}$`)
var publicHostname = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?\.[a-z]{2,63}$`)
var templateVariable = regexp.MustCompile(`\$(?:\{[1-9][0-9]*\}|[1-9][0-9]*)`)

func ValidExternalID(value string) bool { return externalIdentity.MatchString(value) }

func referencePattern(pattern string) (*regexp.Regexp, error) {
	if len(pattern) > 512 || !strings.HasPrefix(pattern, "^") || !strings.HasSuffix(pattern, "$") {
		return nil, invalidResult()
	}
	return regexp.Compile(pattern)
}

func safeDisplayURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	return u, err == nil && len(raw) <= 2048 && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.Fragment == "" && publicHostname.MatchString(u.Host) && !strings.Contains(u.Host, "..")
}

func (p ResourcePolicy) Validate() error {
	for _, rules := range [][]ReferenceRule{p.Tracks, p.Playlists, p.Albums, p.Artists} {
		if len(rules) > 16 {
			return invalidResult()
		}
		for _, rule := range rules {
			re, err := referencePattern(rule.IDPattern)
			// 模板变量只能出现在固定主机之后，禁止插件以身份替换主机。
			u, ok := safeDisplayURL(templateVariable.ReplaceAllString(rule.URLTemplate, "1"))
			if err != nil || re.NumSubexp() < 1 || !ok || strings.Contains(strings.SplitN(strings.TrimPrefix(rule.URLTemplate, "https://"), "/", 2)[0], "$") || u.Path == "" {
				return invalidResult()
			}
			variables := templateVariable.FindAllString(rule.URLTemplate, -1)
			if len(variables) == 0 || strings.Contains(templateVariable.ReplaceAllString(rule.URLTemplate, ""), "$") {
				return invalidResult()
			}
			for _, variable := range variables {
				index, err := strconv.Atoi(strings.Trim(variable, "${}"))
				if err != nil || index > re.NumSubexp() {
					return invalidResult()
				}
			}
		}
	}
	if len(p.PlaylistEntryPatterns) > 16 || len(p.ImageHosts) > 16 {
		return invalidResult()
	}
	for _, pattern := range p.PlaylistEntryPatterns {
		if _, err := referencePattern(pattern); err != nil {
			return invalidResult()
		}
	}
	for _, host := range p.ImageHosts {
		if len(host) > 253 || !publicHostname.MatchString(strings.TrimPrefix(host, "*.")) || strings.Contains(host, "..") {
			return invalidResult()
		}
	}
	return nil
}

// ReferenceURL 返回绑定到该身份的唯一资料链接；空字符串表示身份未被声明支持。
func ReferenceURL(rules []ReferenceRule, id string) string {
	if !ValidExternalID(id) {
		return ""
	}
	for _, rule := range rules {
		re, err := referencePattern(rule.IDPattern)
		if err != nil {
			continue
		}
		if match := re.FindStringSubmatchIndex(id); match != nil {
			result := string(re.ExpandString(nil, rule.URLTemplate, id, match))
			if _, ok := safeDisplayURL(result); ok {
				return result
			}
		}
	}
	return ""
}

func (p ResourcePolicy) ValidPlaylistEntry(id string) bool {
	if ReferenceURL(p.Tracks, id) != "" {
		return true
	}
	if !ValidExternalID(id) {
		return false
	}
	for _, pattern := range p.PlaylistEntryPatterns {
		if re, err := referencePattern(pattern); err == nil && re.MatchString(id) {
			return true
		}
	}
	return false
}

func (p ResourcePolicy) ImageURL(raw string) (*string, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, UpstreamInvalid()
	}
	// 官方图片可从 HTTP 规范化为 HTTPS，返回值仍只允许安全的展示地址。
	if u.Scheme == "http" {
		u.Scheme = "https"
	}
	if _, ok := safeDisplayURL(u.String()); !ok || u.RawQuery != "" || u.ForceQuery || u.Path == "" || u.Path == "/" {
		return nil, UpstreamInvalid()
	}
	for _, host := range p.ImageHosts {
		if u.Host == host || strings.HasPrefix(host, "*.") && strings.HasSuffix(u.Host, host[1:]) {
			value := u.String()
			return &value, nil
		}
	}
	return nil, UpstreamInvalid()
}
