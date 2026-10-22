package config_test

import "testing"

func TestServerEffectsDefaultDisabledRetainsConfiguredChain(t *testing.T) {
	for _, enabled := range []string{"", "enabled: false,"} {
		raw := minimalYAML + "audioDSP: {" + enabled + "chain: [{name: reverb, version: 1}]}\n"
		doc, err := parse(t, raw)
		if err != nil || doc.Config.AudioDSP.Enabled || len(doc.Config.AudioDSP.Chain) != 1 {
			t.Fatalf("disabled configuration activated effects or discarded retained settings: %+v %v", doc, err)
		}
		validateDSPConfigSchema(t, raw, true)
	}
	if _, err := parse(t, minimalYAML+"audioDSP: {enabled: true, chain: [{name: reverb, version: 1}]}\n"); err == nil {
		t.Fatal("enabled effects accepted without an audio processor")
	}
}
