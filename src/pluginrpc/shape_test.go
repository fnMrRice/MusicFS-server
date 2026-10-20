package pluginrpc

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func TestWirePreservesNestedEmptyCollectionsAndZeroPointers(t *testing.T) {
	no := false
	zero := 0
	empty := ""
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
	input := snapshot{
		Internal:   "private-generation",
		EmptySlice: []string{}, EmptyMap: map[string]string{},
		Overrides: pluginproto.ImportOverrides{Recursive: &no, MaxDepth: &zero, SkipHidden: &no, RespectNoMedia: &no},
		Zero:      &zero, Empty: &empty,
		Nested:  []map[string][]string{{"empty": {}, "nil": nil}, {}, nil},
		Dynamic: map[string]any{"empty": []any{}, "map": map[string]any{}, "zero": false, "nil": nil},
	}
	endpoint := NewEndpoint()
	var cleanup []string
	wire, err := endpoint.encode(context.Background(), reflect.ValueOf(input), &cleanup)
	if err != nil {
		t.Fatal(err)
	}
	result, err := endpoint.decode(context.Background(), wire, reflect.TypeOf(input))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, result.Interface()) {
		t.Fatalf("wire changed immutable snapshot shape:\nwant: %#v\ngot: %#v", input, result.Interface())
	}
}

func TestShapeNestingIsBounded(t *testing.T) {
	var nested any = []any{}
	for range 70 {
		nested = []any{nested}
	}
	if _, err := captureShape(reflect.ValueOf(nested)); err == nil {
		t.Fatal("unbounded persistence shape nesting was accepted")
	}
}

func TestCyclicValueIsRejectedBeforeGobEncoding(t *testing.T) {
	type cyclic struct{ Next *cyclic }
	input := &cyclic{}
	input.Next = input
	var cleanup []string
	if _, err := NewEndpoint().encode(context.Background(), reflect.ValueOf(input), &cleanup); err == nil {
		t.Fatal("cyclic persistence value was accepted")
	}
}
