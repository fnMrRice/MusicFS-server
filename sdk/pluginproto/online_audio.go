package pluginproto

import "regexp"

const OnlineAudioChunkBytes = 64 * 1024
const MaxOnlineAudioBytes int64 = 256 * 1024 * 1024

// OnlineAudio 仅传递完整音轨的描述；网络地址和请求凭据留在插件内部。
type OnlineAudio struct {
	ExternalID   string `json:"externalId"`
	Quality      string `json:"quality,omitempty"`
	Version      string `json:"version,omitempty"`
	MIME         string `json:"mime"`
	Codec        string `json:"codec"`
	Container    string `json:"container"`
	SizeBytes    int64  `json:"sizeBytes"`
	DurationMs   *int64 `json:"durationMs"`
	BitrateBps   *int64 `json:"bitrateBps"`
	SampleRateHz int    `json:"sampleRateHz,omitempty"`
}

func (a OnlineAudio) Validate() error {
	if !OnlineIdentity(a.ExternalID) || a.SizeBytes < -1 || a.SizeBytes > MaxOnlineAudioBytes || !validAudioTag(a.Quality) || !validAudioTag(a.Version) {
		return Failure("PROTOCOL_MISMATCH", "Invalid online audio identity or size")
	}
	valid := a.MIME == "audio/mp4" && a.Container == "m4a" && a.Codec == "aac" || a.MIME == "audio/mpeg" && a.Container == "mp3" && a.Codec == "mp3" || a.MIME == "audio/flac" && a.Container == "flac" && a.Codec == "flac" || a.MIME == "audio/wav" && a.Container == "wav" && a.Codec == "pcm_s16le"
	if !valid || a.SampleRateHz < 0 || a.SampleRateHz > 1048575 || a.DurationMs != nil && (*a.DurationMs <= 0 || *a.DurationMs > 24*60*60*1000) || a.BitrateBps != nil && (*a.BitrateBps <= 0 || *a.BitrateBps > 100000000) {
		return Failure("PROTOCOL_MISMATCH", "Invalid online audio format")
	}
	return nil
}

// OnlineAudioReference 的音质仅作用于本次播放，实际音质由插件在响应中声明。
type OnlineAudioReference struct {
	Context    Context `json:"context"`
	ExternalID string  `json:"externalId"`
	Quality    string  `json:"quality,omitempty"`
}

func (r OnlineAudioReference) Validate() error {
	if !OnlineIdentity(r.ExternalID) || !validAudioTag(r.Quality) {
		return Failure("INVALID_ARGUMENT", "Invalid online audio request")
	}
	return nil
}

var audioTag = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validAudioTag(value string) bool { return value == "" || audioTag.MatchString(value) }

type OnlineAudioOpened struct {
	Audio    OnlineAudio `json:"audio"`
	StreamID string      `json:"streamId"`
}

type OnlineAudioRead struct {
	Context  Context `json:"context"`
	StreamID string  `json:"streamId"`
	Offset   int64   `json:"offset"`
}

type OnlineAudioChunk struct {
	Offset int64  `json:"offset"`
	Data   []byte `json:"data"`
	EOF    bool   `json:"eof"`
}

var onlineAudioID = regexp.MustCompile(`^[0-9a-f]{48}$`)

func ValidOnlineAudioID(id string) bool { return onlineAudioID.MatchString(id) }
