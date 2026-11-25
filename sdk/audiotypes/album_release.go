package audiotypes

// AlbumRelease 保存原生曲目资料提供的发行版标识。不同平台的相同编号是不同发行版。
type AlbumRelease struct {
	Namespace string `json:"namespace"`
	ID        string `json:"id"`
}

// AddAlbumRelease 合并同一音源不同层的发行信息。有冲突时，后续标签不能覆盖该状态。
func (m *Metadata) AddAlbumRelease(r *AlbumRelease) {
	if m.AlbumReleaseConflict || r == nil || !r.Valid() {
		return
	}
	if m.AlbumRelease != nil && *m.AlbumRelease != *r {
		m.AlbumRelease, m.AlbumReleaseConflict = nil, true
		return
	}
	m.AlbumRelease = r
}

// Valid 按平台所用的标识格式校验。QQ 音乐使用专辑 MID，网易云使用十进制编号。
func (r AlbumRelease) Valid() bool {
	switch r.Namespace {
	case "netease":
		return ValidNetEaseID(r.ID)
	case "qqmusic":
		return ValidQQMusicMID(r.ID)
	}
	return false
}

// ValidQQMusicMID 校验平台曲目和专辑使用的 MID。
func ValidQQMusicMID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
