// Package driver implements QQ Music authentication and desktop metadata RPC.
// Its dependencies are explicit; it has no dependency on another platform.
package driver

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/session"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

// Client receives the runtime's isolated account state and bounded services.
type Client struct {
	session.Unsupported
	session.Runtime
}

func New(runtime session.Runtime) session.Driver {
	return &Client{Runtime: runtime}
}

var numericID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var songMID = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

type artist struct {
	Name string `json:"name"`
}
type album struct {
	Name string `json:"name"`
}

func captureCookies(res *http.Response, cookies map[string]string) {
	for _, cookie := range res.Cookies() {
		if len(cookie.Value) <= 16384 && cookie.Valid() == nil {
			if cookie.MaxAge < 0 {
				delete(cookies, cookie.Name)
			} else {
				cookies[cookie.Name] = cookie.Value
			}
		}
	}
}

func decodeAccount(raw []byte, out any) error {
	if json.Unmarshal(raw, out) != nil {
		return online.UpstreamInvalid()
	}
	return nil
}
