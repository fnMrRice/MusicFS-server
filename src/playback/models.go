package playback

import "time"

type Format struct {
	MIME    string `json:"mime"`
	CanPlay string `json:"canPlay"`
}
type Capabilities struct {
	HTMLAudio  bool     `json:"htmlAudio,omitempty"`
	NativeHTTP bool     `json:"nativeHttp,omitempty"`
	Formats    []Format `json:"formats"`
}

func (c Capabilities) transport() string {
	if c.NativeHTTP {
		return "nativeHttp"
	}
	return "htmlAudio"
}

type Preferences struct {
	ForceProcessing bool    `json:"forceProcessing"`
	GainDB          float64 `json:"gainDb"`
	DSPExecution    string  `json:"dspExecution"`
}
type CreateRequest struct {
	TrackID        string       `json:"trackId,omitempty"`
	ForcedSourceID string       `json:"forcedSourceTrackId,omitempty"`
	SourceTrackID  string       `json:"sourceTrackId"`
	Capabilities   Capabilities `json:"capabilities"`
	Preferences    Preferences  `json:"preferences"`
}
type OutputProfile struct {
	ID         string  `json:"id,omitempty"`
	MIME       string  `json:"mime"`
	Codec      *string `json:"codec"`
	Container  *string `json:"container"`
	Transport  string  `json:"transport"`
	SampleRate int     `json:"sampleRate,omitempty"`
	Channels   int     `json:"channels,omitempty"`
	Layout     string  `json:"layout,omitempty"`
	BitrateBps int     `json:"bitrateBps,omitempty"`
}
type Range struct {
	StartMs int64 `json:"startMs"`
	EndMs   int64 `json:"endMs"`
}
type Processing struct {
	Type       string             `json:"type"`
	GainDB     float64            `json:"gainDb"`
	Version    string             `json:"version"`
	Execution  string             `json:"execution"`
	Parameters map[string]float64 `json:"parameters,omitempty"`
}
type Description struct {
	SourceTrackID       string        `json:"sourceTrackId"`
	DSPExecution        string        `json:"dspExecution"`
	SessionID           string        `json:"sessionId"`
	AttemptID           string        `json:"attemptId"`
	AssetID             string        `json:"assetId"`
	RepresentationID    string        `json:"representationId"`
	DeliveryMode        string        `json:"deliveryMode"`
	OutputProfile       OutputProfile `json:"outputProfile"`
	StreamURL           string        `json:"streamUrl"`
	ExpiresAt           time.Time     `json:"expiresAt"`
	SeekMode            string        `json:"seekMode"`
	SeekableRange       *Range        `json:"seekableRange"`
	PositionOriginMs    int64         `json:"positionOriginMs"`
	EffectiveProcessing []Processing  `json:"effectiveProcessing"`
	ResumeDecision      string        `json:"resumeDecision"`
}
type ObservedError struct {
	Code          string         `json:"code"`
	Message       string         `json:"message"`
	OutputProfile *OutputProfile `json:"outputProfile,omitempty"`
}
type ControlRequest struct {
	AttemptID     string         `json:"attemptId"`
	PositionMs    int64          `json:"positionMs"`
	ObservedError *ObservedError `json:"observedError,omitempty"`
}
