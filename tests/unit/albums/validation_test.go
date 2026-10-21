package albums_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/albums"
)

func TestAlbumInputsHaveBoundedCanonicalIdentity(t *testing.T) {
	ids := []string{"b", "a"}
	got, err := albums.NormalizeMembers(ids, 2)
	if err != nil || !reflect.DeepEqual(got, []string{"b", "a"}) || ids[0] != "b" {
		t.Fatal("normalization changed member order", err)
	}
	got[0] = "changed"
	if ids[0] != "b" {
		t.Fatal("normalization aliased the caller")
	}
	for _, bad := range [][]string{{"a", "a"}, {"a", "b", "c"}, {""}, {"x\n"}, {" space"}} {
		if _, err = albums.NormalizeMembers(bad, 2); err == nil {
			t.Fatal("invalid membership accepted", bad)
		}
	}
	for _, bad := range []string{"", strings.Repeat("字", 257), "name\nline", string([]byte{255})} {
		if _, err = albums.NormalizeName(bad); err == nil {
			t.Fatal("invalid display name accepted")
		}
	}
	if got, err := albums.NormalizeName(" 中文专辑 "); err != nil || got != "中文专辑" {
		t.Fatal(err)
	}
	if err = (albums.Target{Kind: "recording", ID: "a"}).Validate(); err == nil {
		t.Fatal("recording used as release identity")
	}
}
