package scraperlocal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

const PluginID = "musicfs.scraper.local"
const channelChunk = 64 * 1024

type helloRequest struct {
	pluginproto.Hello
	Context pluginproto.Context `json:"context"`
	Target  enrichment.Target   `json:"target"`
	BaseURL string              `json:"baseUrl"`
}

func decode(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("PROTOCOL_MISMATCH: invalid scraper request")
	}
	return nil
}

func validateBaseURL(raw, nonce string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || !enrichment.ValidRef(nonce) || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.EscapedPath() != "/"+nonce {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid scraper data channel")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 || u.Host != "127.0.0.1:"+strconv.Itoa(port) {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid scraper channel port")
	}
	return u, nil
}
