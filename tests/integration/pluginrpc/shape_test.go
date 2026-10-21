package pluginrpc_test

import (
	"context"
	"net"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/pluginrpc"
)

type echoContract[T any] interface {
	Echo(context.Context, T) (T, error)
}
type echoService[T any] struct{}

func (echoService[T]) Echo(_ context.Context, v T) (T, error) { return v, nil }

func roundTrip[T any](t *testing.T, input T) (T, error) {
	t.Helper()
	left, right := net.Pipe()
	client, server := pluginrpc.NewEndpoint(), pluginrpc.NewEndpoint()
	contract := reflect.TypeFor[echoContract[T]]()
	if err := client.SetContract(contract); err != nil {
		t.Fatal(err)
	}
	if err := server.Bind(echoService[T]{}, contract); err != nil {
		t.Fatal(err)
	}
	client.Peer = pluginproto.NewPeerWithLimit(t.Context(), left, left, "h-", client.Handle, pluginrpc.MaxMessageBytes)
	server.Peer = pluginproto.NewPeerWithLimit(t.Context(), right, right, "w-", server.Handle, pluginrpc.MaxMessageBytes)
	defer client.Peer.Close()
	defer server.Peer.Close()
	var result T
	err := client.Invoke(t.Context(), "Echo", []any{input}, []any{&result})
	return result, err
}

type snapshot struct {
	Internal   string `json:"-"`
	NilSlice   []string
	EmptySlice []string
	EmptyMap   map[string]string
	NilMap     map[string]string
	Overrides  pluginproto.ImportOverrides
	Zero       *int
	Empty      *string
	Nested     []map[string][]string
	Dynamic    map[string]any
}

func TestWirePreservesNestedEmptyCollectionsAndZeroPointers(t *testing.T) {
	no, zero, empty := false, 0, ""
	input := snapshot{
		Internal: "private-generation", EmptySlice: []string{}, EmptyMap: map[string]string{},
		Overrides: pluginproto.ImportOverrides{Recursive: &no, MaxDepth: &zero, SkipHidden: &no, RespectNoMedia: &no},
		Zero:      &zero, Empty: &empty,
		Nested:  []map[string][]string{{"empty": {}, "nil": nil}, {}, nil},
		Dynamic: map[string]any{"empty": []any{}, "map": map[string]any{}, "zero": false, "nil": nil},
	}
	result, err := roundTrip(t, input)
	if err != nil || !reflect.DeepEqual(input, result) {
		t.Fatalf("wire changed immutable snapshot shape: want %#v, got %#v, error %v", input, result, err)
	}
}

func TestShapeNestingIsBounded(t *testing.T) {
	var nested any = []any{}
	for range 70 {
		nested = []any{nested}
	}
	if _, err := roundTrip(t, nested); err == nil {
		t.Fatal("unbounded persistence shape nesting was accepted")
	}
}

func TestCyclicValueIsRejectedBeforeGobEncoding(t *testing.T) {
	type cyclic struct{ Next *cyclic }
	input := &cyclic{}
	input.Next = input
	if _, err := roundTrip(t, input); err == nil {
		t.Fatal("cyclic persistence value was accepted")
	}
}
