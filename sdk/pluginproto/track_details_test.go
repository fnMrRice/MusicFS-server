package pluginproto_test

import (
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestTrackDetailsValidationRejectsWrongIdentityAndMalformedFields(t *testing.T) {
	valid := func() pluginproto.TrackDetails {
		return pluginproto.TrackDetails{ExternalID: "42", Title: "曲目", Sections: []pluginproto.TrackDetailSection{{ID: "custom", Title: "插件信息", Fields: []pluginproto.TrackDetailField{{ID: "note", Label: "说明", Value: "第一行\n第二行"}}}}}
	}
	if err := valid().Validate("42"); err != nil {
		t.Fatal(err)
	}
	empty := valid()
	empty.Sections = []pluginproto.TrackDetailSection{}
	if err := empty.Validate("42"); err != nil {
		t.Fatal("empty plugin detail should remain a valid result", err)
	}
	for name, mutate := range map[string]func(*pluginproto.TrackDetails){
		"wrong identity":     func(d *pluginproto.TrackDetails) { d.ExternalID = "43" },
		"missing sections":   func(d *pluginproto.TrackDetails) { d.Sections = nil },
		"empty section":      func(d *pluginproto.TrackDetails) { d.Sections[0].Fields = nil },
		"duplicate sections": func(d *pluginproto.TrackDetails) { d.Sections = append(d.Sections, d.Sections[0]) },
		"duplicate fields": func(d *pluginproto.TrackDetails) {
			d.Sections[0].Fields = append(d.Sections[0].Fields, d.Sections[0].Fields[0])
		},
		"invalid identifier": func(d *pluginproto.TrackDetails) { d.Sections[0].Fields[0].ID = "bad id" },
		"control character":  func(d *pluginproto.TrackDetails) { d.Sections[0].Fields[0].Value = "secret\x00" },
		"oversized value":    func(d *pluginproto.TrackDetails) { d.Sections[0].Fields[0].Value = strings.Repeat("x", 16385) },
	} {
		t.Run(name, func(t *testing.T) {
			d := valid()
			mutate(&d)
			if err := d.Validate("42"); pluginproto.ErrorCode(err) != "PROTOCOL_MISMATCH" {
				t.Fatal("invalid provider details were accepted", err)
			}
		})
	}
}
