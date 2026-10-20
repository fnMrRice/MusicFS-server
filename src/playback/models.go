package playback

import "time"

type Format struct {
	MIME    string `json:"mime"`
	CanPlay string `json:"canPlay"`
}
type Capabilities struct {
	HTMLAudio bool     `json:"htmlAudio"`
	Formats   []Format `json:"formats"`
}
type Preferences struct {
	ForceProcessing bool    `json:"forceProcessing"`
	GainDB          float64 `json:"gainDb"`
}
type CreateRequest struct {
	SourceTrackID string       `json:"sourceTrackId"`
	Capabilities  Capabilities `json:"capabilities"`
	Preferences   Preferences  `json:"preferences"`
}
type OutputProfile struct {
	MIME      string  `json:"mime"`
	Codec     *string `json:"codec"`
	Container *string `json:"container"`
	Transport string  `json:"transport"`
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
