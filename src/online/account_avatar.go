package online

import "net/url"

// Avatars are optional display references, never server-side fetch destinations.
// Ignore unsupported or malformed images without failing account authorization.
func accountAvatarURL(platform, raw string) string {
	if platform != Netease || raw == "" || len(raw) > 2048 {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || len(u.Path) < 2 {
		return ""
	}
	switch u.Host {
	case "p1.music.126.net", "p2.music.126.net", "p3.music.126.net", "p4.music.126.net":
		u.Scheme = "https"
		if normalized := u.String(); len(normalized) <= 2048 {
			return normalized
		}
		return ""
	default:
		return ""
	}
}
