package online_test

import (
	"context"
	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
	"testing"
)

func TestDefaultTransportsDoNotEnableAnonymousAccess(t *testing.T) {
	for _, platform := range []string{online.Netease, online.QQMusic, online.AppleMusic} {
		c, err := onlineplugins.New(platform, online.Scope{UserID: "validation", InstanceID: "source"}, online.Options{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Search(context.Background(), online.SearchRequest{Query: "sample", Limit: 1})
		expectCode(t, err, "PLATFORM_AUTH_REQUIRED")
		c.Close()
	}
}
