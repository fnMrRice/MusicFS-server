package pluginhost

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func displayCountFixture(value int64) *plugindisplay.Count {
	return &plugindisplay.Count{Value: value, Unit: "custom units", LocalizedUnits: map[string]string{"zh": "个自定义单位"}}
}

func TestLocalDisplayCountCachedWithoutRPCAndOptionalForOldWorker(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		left, right := net.Pipe()
		host := pluginproto.NewPeer(ctx, left, left, "h-", nil)
		worker := pluginproto.NewPeer(ctx, right, right, "w-", func(_ context.Context, method string, raw json.RawMessage) (any, error) {
			if method != "plugin.displayCount" {
				t.Error("unexpected method", method)
			}
			var request pluginproto.DisplayCountRequest
			if json.Unmarshal(raw, &request) != nil || request.InstanceID != "local" || len(request.RootIDs) != 2 {
				t.Error("root identities were not supplied", string(raw))
			}
			if unsupported {
				return nil, pluginproto.Failure("UNSUPPORTED", "Older worker")
			}
			return displayCountFixture(7), nil
		})
		w := &Worker{peer: host, done: make(chan struct{}), config: Config{InstanceID: "local", Roots: []sourceaccess.RootConfig{{ID: "one"}, {ID: "two"}}}}
		if err := w.loadDisplayCount(ctx); err != nil {
			t.Fatal(err)
		}
		host.Close()
		worker.Close()
		cancel()
		count := w.DisplayCount()
		if unsupported {
			if count != nil {
				t.Fatal("old worker acquired a fabricated statistic")
			}
		} else {
			if count == nil || count.Value != 7 || count.Unit != "custom units" {
				t.Fatal("host derived or lost the plugin count", count)
			}
			count.LocalizedUnits["zh"] = "changed"
			if w.DisplayCount().LocalizedUnits["zh"] != "个自定义单位" {
				t.Fatal("cached statistic was exposed for mutation")
			}
		}
		close(w.done)
		if w.DisplayCount() != nil {
			t.Fatal("stopped local worker retained a statistic")
		}
	}
}

func TestOnlineDisplayCountFollowsAccountGenerationsAndExpiry(t *testing.T) {
	w := &OnlineWorker{done: make(chan struct{})}
	state := &w.accountDisplay
	initial := state.begin(false)
	state.finish(initial, false, signedInDisplay(), displayCountFixture(9), true)
	if got := w.DisplayCount(); got == nil || got.Value != 9 {
		t.Fatal("host must retain the plugin value without deriving a user count", got)
	}
	stale := state.begin(false)
	state.invalidate()
	state.finish(stale, false, signedInDisplay(), displayCountFixture(9), true)
	if w.DisplayCount() != nil {
		t.Fatal("pre-logout result restored a stale count")
	}
	changing := state.begin(true)
	concurrent := state.begin(false)
	state.finish(concurrent, false, signedInDisplay(), displayCountFixture(9), true)
	if w.DisplayCount() != nil {
		t.Fatal("pending account change published a count")
	}
	state.finish(changing, true, online.AccountView{State: "signedOut"}, displayCountFixture(0), true)
	if got := w.DisplayCount(); got == nil || got.Value != 0 {
		t.Fatal("plugin-provided signed-out zero was lost", got)
	}
	past := time.Now().Add(-time.Second)
	expired := signedInDisplay()
	expired.ExpiresAt = &past
	token := state.begin(false)
	state.finish(token, false, expired, displayCountFixture(9), true)
	if w.DisplayCount() != nil {
		t.Fatal("expired account retained the old count")
	}
	token = state.begin(false)
	state.finish(token, false, signedInDisplay(), displayCountFixture(9), true)
	close(w.done)
	if w.DisplayCount() != nil {
		t.Fatal("stopped online worker retained a statistic")
	}
}

func TestMalformedOptionalAccountCountPreservesValidAccountResult(t *testing.T) {
	for _, invalid := range []string{
		`{"value":-1,"unit":"users","localizedUnits":{}}`,
		`{"value":"invalid","unit":"users","localizedUnits":{}}`,
		`{"value":1,"unit":"users"}`,
		`{"unit":"users","localizedUnits":{}}`,
		`null`,
	} {
		t.Run(invalid, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			left, right := net.Pipe()
			host := pluginproto.NewPeer(ctx, left, left, "h-", nil)
			defer host.Close()
			worker := pluginproto.NewPeer(ctx, right, right, "w-", func(_ context.Context, _ string, raw json.RawMessage) (any, error) {
				var request pluginproto.OnlineAccount
				if err := json.Unmarshal(raw, &request); err != nil {
					return nil, err
				}
				return pluginproto.OnlineResult[any]{Context: request.Context, Value: map[string]any{
					"view": signedInDisplay(), "displayCount": json.RawMessage(invalid),
				}}, nil
			})
			defer worker.Close()
			w := &OnlineWorker{peer: host, done: make(chan struct{}), config: OnlineConfig{InstanceID: "online", UserID: "admin", Timeout: time.Second}}
			token := w.accountDisplay.begin(false)
			w.accountDisplay.finish(token, false, signedInDisplay(), displayCountFixture(9), true)
			result, err := w.accountCall(ctx, online.AccountRequest{Action: "state"})
			if err != nil || result.View.AccountID == "" || result.DisplayCount != nil || w.DisplayCount() != nil || w.PlatformSessionSnapshot().AccountID == "" {
				t.Fatal("optional statistic corrupted account result or retained old data", err, result)
			}
		})
	}
}
