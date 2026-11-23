package online

import "time"

// DailySongsPage 保留当前账号的每日推荐歌曲及平台给出的顺序。
type DailySongsPage struct {
	Tracks     []Track   `json:"tracks"`
	ObservedAt time.Time `json:"observedAt"`
}

func ValidateDailySongs(policy ResourcePolicy, page DailySongsPage, limit int) error {
	if limit < 1 || limit > MaxPageItems || page.Tracks == nil || len(page.Tracks) > limit || page.ObservedAt.IsZero() {
		return invalidResult()
	}
	for _, track := range page.Tracks {
		if err := ValidateTrack(policy, track); err != nil {
			return err
		}
	}
	return nil
}
