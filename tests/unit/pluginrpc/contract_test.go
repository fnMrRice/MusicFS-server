package pluginrpc_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginrpc"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func TestPersistenceFingerprintsSurviveSourceLayoutMigration(t *testing.T) {
	// Captured from the real contracts before the source directory migration.
	for _, test := range []struct {
		name     string
		contract reflect.Type
		want     string
	}{
		{"backend", reflect.TypeFor[storage.Backend](), "2d73311980654791060d6cb15deef78194c48d7f18cfccae4bf75ec3310c1831"},
		{"maintenance", reflect.TypeFor[storage.Maintenance](), "c2b65a92a0386d7990000f825babaf527dfb541184711fca13d26cc5298fa013"},
		{"plugin-state", reflect.TypeFor[plugindb.Backend](), "3585f631ae09e5d4694d439d5d2a72e5d8d2f30cf9b64ac2003dd674eb12c401"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := pluginrpc.Fingerprint(test.contract); got != test.want {
				t.Fatalf("persistence contract identity changed: got %s, want %s", got, test.want)
			}
		})
	}
}

func TestFingerprintStillDetectsContractShapeChanges(t *testing.T) {
	base := reflect.TypeFor[interface {
		Save(context.Context, struct {
			Generation string `json:"-"`
		}) error
	}]()
	for _, test := range []struct {
		name     string
		contract reflect.Type
	}{
		{"internal-field-type", reflect.TypeFor[interface {
			Save(context.Context, struct {
				Generation int64 `json:"-"`
			}) error
		}]()},
		{"internal-field-name", reflect.TypeFor[interface {
			Save(context.Context, struct {
				Revision string `json:"-"`
			}) error
		}]()},
		{"method-name", reflect.TypeFor[interface {
			Load(context.Context, struct {
				Generation string `json:"-"`
			}) error
		}]()},
		{"method-added", reflect.TypeFor[interface {
			Save(context.Context, struct {
				Generation string `json:"-"`
			}) error
			Close() error
		}]()},
		{"return-type", reflect.TypeFor[interface {
			Save(context.Context, struct {
				Generation string `json:"-"`
			}) (bool, error)
		}]()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if pluginrpc.Fingerprint(test.contract) == pluginrpc.Fingerprint(base) {
				t.Fatal("different persistence contract shapes have the same fingerprint")
			}
		})
	}
}

type firstRecordID string
type secondRecordID string

func TestFingerprintRetainsNamedTypeIdentity(t *testing.T) {
	first := reflect.TypeFor[interface{ Get() firstRecordID }]()
	second := reflect.TypeFor[interface{ Get() secondRecordID }]()
	if pluginrpc.Fingerprint(first) == pluginrpc.Fingerprint(second) {
		t.Fatal("different named DTO types have the same fingerprint")
	}
}
