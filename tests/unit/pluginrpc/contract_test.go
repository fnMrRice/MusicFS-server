package pluginrpc_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginrpc"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func TestPersistenceContractFingerprints(t *testing.T) {
	// 主库曲目和队列条目携带在线封面入口，原始平台地址只在服务端传递。
	// 其余接口继续保留源码目录迁移前的指纹，目录调整不应改变协议身份。
	for _, test := range []struct {
		name     string
		contract reflect.Type
		want     string
	}{
		{"backend", reflect.TypeFor[storage.Backend](), "3ee7e3accf50a3b6be034fcaa35c4394d065029fc428dd01444cbca575391dcf"},
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
