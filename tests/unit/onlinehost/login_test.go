package onlinehost_test

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"testing"
)

func TestAnonymousPluginDoesNotAdvertiseLogin(t *testing.T) {
	host, provider := anonymousHost(t)
	options, err := host.AccountOptions(context.Background(), "owner", "bili")
	if err != nil || options.Method != "none" || options.Configured || options.DeveloperToken != "" || provider.calls != 0 {
		t.Fatalf("anonymous plugin advertised login or queried account: %+v %v", options, err)
	}
	if _, err := host.AccountOptions(context.Background(), "other", "bili"); err == nil {
		t.Fatal("login options escaped owner scope")
	}
}

type customLogin struct{ describedProvider }

func (p *customLogin) PlatformAccountOptions(context.Context) (online.AccountOptions, error) {
	return online.AccountOptions{Method: "deviceCode", Configured: true, Validation: "verified"}, nil
}

func TestPluginDeclaresLoginWithoutHostPlatformBranch(t *testing.T) {
	_, base := anonymousHost(t)
	d := base.Descriptor()
	d.Login = &online.LoginDescriptor{Method: "deviceCode", Validation: "verified"}
	p := &customLogin{describedProvider{blocked: &blocked{}, descriptor: d}}
	host, err := onlinehost.New([]onlinehost.Runtime{{Owner: "owner", InstanceID: "custom", Platform: online.Bilibili, ConfigRevision: "1", Enabled: true, Provider: p}})
	if err != nil {
		t.Fatal(err)
	}
	options, err := host.AccountOptions(context.Background(), "owner", "custom")
	if err != nil || options.Method != "deviceCode" || !options.Configured {
		t.Fatal("host replaced plugin login method", options, err)
	}
}
