package pluginhost_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

func TestLocalDisplayCountCachedWithoutRPCAndOptionalForOldWorker(t *testing.T) {
	for _, old := range []bool{false, true} {
		mode := "local"
		if old {
			mode = "old-worker"
		}
		w := startLocal(t, localConfig(t, mode))
		count := w.DisplayCount()
		if old {
			if count != nil {
				t.Fatal("old worker acquired a fabricated statistic")
			}
		} else {
			if count == nil || count.Value != 7 || count.Unit != "custom units" {
				t.Fatal("host derived or lost plugin count", count)
			}
			count.LocalizedUnits["zh"] = "changed"
			if w.DisplayCount().LocalizedUnits["zh"] != "个自定义单位" {
				t.Fatal("cached statistic was exposed for mutation")
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if w.DisplayCount() != nil {
			t.Fatal("stopped local worker retained a statistic")
		}
	}
}

func TestOnlineDisplayCountFollowsAccountGenerationsAndExpiry(t *testing.T) {
	w, dir := startOnline(t)
	if got := w.DisplayCount(); got == nil || got.Value != 9 {
		t.Fatal("host must retain plugin value", got)
	}
	count := w.DisplayCount()
	count.LocalizedUnits["zh"] = "changed"
	if w.DisplayCount().LocalizedUnits["zh"] != "个自定义单位" {
		t.Fatal("cached online count was exposed for mutation")
	}
	signedOut := online.AccountView{State: "signedOut"}
	setControl(t, dir, fixtureControl{View: &signedOut, Count: json.RawMessage(`{"value":0,"unit":"custom units","localizedUnits":{}}`)})
	if _, err := w.PlatformAccount(t.Context(), online.AccountRequest{Action: "logout"}); err != nil {
		t.Fatal(err)
	}
	if got := w.DisplayCount(); got == nil || got.Value != 0 {
		t.Fatal("plugin-provided signed-out zero was lost", got)
	}
	expired := signedInDisplay()
	past := time.Now().Add(-time.Second)
	expired.ExpiresAt = &past
	setControl(t, dir, fixtureControl{View: &expired})
	if _, err := w.PlatformSession(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w.DisplayCount() != nil {
		t.Fatal("expired account retained old count")
	}
	setControl(t, dir, fixtureControl{})
	if _, err := w.PlatformSession(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if w.DisplayCount() != nil {
		t.Fatal("stopped online worker retained a statistic")
	}
}

func TestMalformedOptionalAccountCountPreservesValidAccountResult(t *testing.T) {
	for _, invalid := range []string{`{"value":-1,"unit":"users","localizedUnits":{}}`, `{"value":"invalid","unit":"users","localizedUnits":{}}`, `{"value":1,"unit":"users"}`, `{"unit":"users","localizedUnits":{}}`, `null`} {
		t.Run(invalid, func(t *testing.T) {
			w, dir := startOnline(t)
			setControl(t, dir, fixtureControl{Count: json.RawMessage(invalid)})
			result, err := w.PlatformSession(t.Context())
			if err != nil || result.AccountID == "" || w.DisplayCount() != nil || w.PlatformSessionSnapshot().AccountID == "" {
				t.Fatal("optional statistic corrupted account result or retained old data", err, result)
			}
		})
	}
}
