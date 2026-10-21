package enrichment_test

import (
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func TestAutomaticPolicyPreservesPriorityAndCanonicalLanguages(t *testing.T) {
	p := enrichment.AutomaticPolicy{Enabled: true, InstancePriority: []string{"z", "a"}, Fields: []string{"lyrics"}, Languages: []string{"ENG", "chi"}}
	n, err := p.Normalize()
	if err != nil || !reflect.DeepEqual(n.InstancePriority, p.InstancePriority) || !reflect.DeepEqual(n.Languages, []string{"en", "zh"}) {
		t.Fatal(n, err)
	}
	n.InstancePriority[0] = "changed"
	if p.InstancePriority[0] != "z" {
		t.Fatal("policy aliases caller data")
	}
	for _, bad := range []enrichment.AutomaticPolicy{{Enabled: true}, {InstancePriority: []string{"a", "a"}}, {InstancePriority: []string{"a", "b", "c", "d", "e"}}, {Fields: []string{"title"}}, {Languages: []string{"eng", "en"}}} {
		if _, err := bad.Normalize(); err == nil {
			t.Fatal("invalid policy accepted", bad)
		}
	}
	if _, err := (enrichment.AutomaticPolicy{}).Normalize(); err != nil {
		t.Fatal(err)
	}
	if mediastore.CanonicalLanguage("lyrics", "ENG-Latn-US") != "en-latn-us" {
		t.Fatal("extended language alias escaped canonical slot")
	}
	for _, alias := range []string{"", "und", "ENG", "en", "zho", "CHI"} {
		want := map[string]string{"": "und", "und": "und", "ENG": "en", "en": "en", "zho": "zh", "CHI": "zh"}[alias]
		if mediastore.CanonicalLanguage("lyrics", alias) != want {
			t.Fatal(alias)
		}
	}
}

func TestAutomaticEligibilityRequiresTrackSpecificEvidence(t *testing.T) {
	id := "preview"
	p := enrichment.AutomaticPolicy{Fields: []string{"lyrics", "artwork"}, InstancePriority: []string{"local"}}
	c := enrichment.PreviewCandidate{Status: "available", PreviewObjectID: &id, InstanceID: "local", Field: "lyrics", Origin: "embedded", Basis: "USLT"}
	if !enrichment.EligibleAutomatic(c, p) {
		t.Fatal("embedded evidence excluded")
	}
	c.Origin = "sidecar"
	c.Basis = "exactStem"
	if !enrichment.EligibleAutomatic(c, p) {
		t.Fatal("same-stem evidence excluded")
	}
	c.Field = "artwork"
	c.Basis = "directoryCover"
	if enrichment.EligibleAutomatic(c, p) {
		t.Fatal("directory cover proves no track identity")
	}
	c.Basis = "exactStem"
	c.InstanceID = "other"
	if enrichment.EligibleAutomatic(c, p) {
		t.Fatal("unselected instance admitted")
	}
}
