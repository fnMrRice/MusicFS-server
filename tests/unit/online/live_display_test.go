package online_test

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"testing"
)

func TestDefaultTransportsDoNotEnableAnonymousAccess(t *testing.T) {
	for _, platform := range []string{online.Netease, online.QQMusic, online.AppleMusic} {
		c, err := online.New(platform, online.Scope{UserID: "validation", InstanceID: "source"}, online.Options{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Search(context.Background(), online.SearchRequest{Query: "sample", Limit: 1})
		expectCode(t, err, "PLATFORM_AUTH_REQUIRED")
		c.Close()
	}
}
