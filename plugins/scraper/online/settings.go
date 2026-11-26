package online

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

type configurablePlugin struct {
	scraper.Plugin
	client  *http.Client
	factory func(*http.Client) scraper.Plugin
}

func configuredPlugin(client *http.Client, factory func(*http.Client) scraper.Plugin) scraper.Plugin {
	return configurablePlugin{Plugin: factory(client), client: client, factory: factory}
}

func (p configurablePlugin) OpenArtwork(ctx context.Context, raw string) (io.ReadCloser, string, error) {
	return scraper.OpenImage(ctx, raw, p.Descriptor().ImageHosts, p.client)
}

func (configurablePlugin) SettingsSchema() pluginsettings.Schema {
	return pluginsettings.Schema{Revision: "scraper-proxy-v1", Title: "网络访问", Description: "为当前账号的这个刮削插件单独设置代理，保存后新请求立即生效。", Fields: []pluginsettings.Field{
		{Key: "proxyMode", Type: "select", Label: "连接方式", Description: "跟随服务端环境使用 HTTP_PROXY、HTTPS_PROXY 和 NO_PROXY；不会读取浏览器或 Windows 的代理设置。", DefaultValue: "environment", Options: []pluginsettings.Option{
			{Value: "environment", Label: "跟随服务端环境"}, {Value: "direct", Label: "直连"}, {Value: "custom", Label: "自定义代理"},
		}},
		{Key: "proxyUrl", Type: "text", Label: "代理地址", Description: "自定义代理示例：http://127.0.0.1:7890 或 socks5://127.0.0.1:1080。支持 HTTP、HTTPS、SOCKS5 和 SOCKS5H；地址中不能包含用户名、密码或查询参数。", DefaultValue: ""},
	}, Actions: []pluginsettings.Action{}}
}

func proxyConfiguration(values map[string]any) (string, *url.URL, error) {
	invalid := func() (string, *url.URL, error) {
		return "", nil, fault.Fail("INVALID_ARGUMENT", "Invalid scraper proxy configuration; use an HTTP, HTTPS or SOCKS5 proxy address without credentials")
	}
	mode, ok := values["proxyMode"].(string)
	if !ok {
		return invalid()
	}
	raw, ok := values["proxyUrl"].(string)
	if !ok || len(raw) > 4096 || strings.ContainsFunc(raw, unicode.IsControl) {
		return invalid()
	}
	if mode != "environment" && mode != "direct" && mode != "custom" {
		return invalid()
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if mode == "custom" {
			return invalid()
		}
		return mode, nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Hostname() == "" || strings.HasSuffix(u.Host, ":") || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return invalid()
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h" {
		return invalid()
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return invalid()
		}
	}
	return mode, u, nil
}

func (p configurablePlugin) ValidateSettings(values map[string]any) error {
	_, _, err := proxyConfiguration(values)
	return err
}

func (p configurablePlugin) WithSettings(values map[string]any) (scraper.Plugin, *http.Client, error) {
	mode, proxy, err := proxyConfiguration(values)
	if err != nil {
		return nil, nil, err
	}
	base := p.client
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	if client.Timeout == 0 {
		client.Timeout = 12 * time.Second
	}
	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if original, ok := transport.(*http.Transport); ok {
		copy := original.Clone()
		switch mode {
		case "environment":
			copy.Proxy = http.ProxyFromEnvironment
		case "direct":
			copy.Proxy = nil
		case "custom":
			copy.Proxy = http.ProxyURL(proxy)
		}
		client.Transport = copy
	} else {
		// 自定义传输器仅用于显式装配；不静默忽略用户要求的代理或直连。
		if mode != "environment" {
			return nil, nil, fault.Fail("UNSUPPORTED", "Scraper transport does not support proxy configuration")
		}
		client.Transport = borrowedTransport{transport}
	}
	return configuredPlugin(&client, p.factory), &client, nil
}

// 非标准传输器由装配方管理；一次任务结束不能关闭其他任务共享的连接池。
type borrowedTransport struct{ http.RoundTripper }
