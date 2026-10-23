package bilibili

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

var number = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var bv = regexp.MustCompile(`^BV[1-9A-HJ-NP-Za-km-z]{10}$`)

type reference struct {
	kind, id, cid string
	page          int
	selected      bool
}

func parseReference(raw string) (reference, error) {
	bad := func() (reference, error) {
		return reference{}, fault.Fail("INVALID_ARGUMENT", "Enter a supported Bilibili ID or canonical HTTPS URL")
	}
	if len(raw) > 2048 || strings.TrimSpace(raw) != raw {
		return bad()
	}
	r := reference{page: 1}
	if strings.HasPrefix(raw, "https://") {
		u, err := url.Parse(raw)
		if err != nil || u.User != nil || u.Port() != "" || u.Fragment != "" || u.RawPath != "" {
			return bad()
		}
		path := strings.TrimSuffix(u.Path, "/")
		switch u.Host {
		case "www.bilibili.com", "bilibili.com":
			switch {
			case strings.HasPrefix(path, "/video/"):
				raw = strings.TrimPrefix(path, "/video/")
			case strings.HasPrefix(path, "/audio/"):
				raw = strings.TrimPrefix(path, "/audio/")
			case strings.HasPrefix(path, "/medialist/detail/ml"):
				raw = "favorite:" + strings.TrimPrefix(path, "/medialist/detail/ml")
			case strings.HasPrefix(path, "/list/ml"):
				raw = "favorite:" + strings.TrimPrefix(path, "/list/ml")
			default:
				return bad()
			}
		case "space.bilibili.com":
			parts := strings.Split(strings.Trim(path, "/"), "/")
			if len(parts) != 2 || !number.MatchString(parts[0]) || parts[1] != "favlist" {
				return bad()
			}
			raw = "favorite:" + u.Query().Get("fid")
		default:
			return bad()
		}
		if values, ok := u.Query()["p"]; ok {
			r.selected = true
			if len(values) != 1 {
				return bad()
			}
			r.page, err = strconv.Atoi(values[0])
			if err != nil || r.page < 1 || r.page > 10000 {
				return bad()
			}
		}
		if values, ok := u.Query()["cid"]; ok {
			if len(values) != 1 || !number.MatchString(values[0]) {
				return bad()
			}
			r.cid = values[0]
		}
	}
	parts := strings.Split(raw, ":")
	if len(parts) == 3 && parts[0] == "video" && bv.MatchString(parts[1]) && number.MatchString(parts[2]) {
		r.kind, r.id, r.cid = "video", parts[1], parts[2]
		return r, nil
	}
	if len(parts) == 2 && (parts[0] == "audio" || parts[0] == "favorite" || parts[0] == "audio-menu") && number.MatchString(parts[1]) {
		r.kind, r.id = parts[0], parts[1]
		return r, nil
	}
	if bv.MatchString(raw) {
		r.kind, r.id = "video", raw
		return r, nil
	}
	for _, pair := range [][2]string{{"av", "video"}, {"au", "audio"}, {"am", "audio-menu"}} {
		if strings.HasPrefix(raw, pair[0]) && number.MatchString(raw[2:]) {
			r.kind, r.id = pair[1], raw[2:]
			return r, nil
		}
	}
	return bad()
}

func (r reference) canonical() string {
	if r.kind == "video" {
		return "video:" + r.id + ":" + r.cid
	}
	return r.kind + ":" + r.id
}

func sourceURL(id string) string {
	parts := strings.Split(id, ":")
	switch parts[0] {
	case "video":
		return "https://www.bilibili.com/video/" + parts[1] + "?cid=" + parts[2]
	case "audio":
		return "https://www.bilibili.com/audio/au" + parts[1]
	case "audio-menu":
		return "https://www.bilibili.com/audio/am" + parts[1]
	default:
		return "https://www.bilibili.com/medialist/detail/ml" + parts[1]
	}
}
