package online_test

import (
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"testing"
)

func TestDeclaredResourcesAcceptNewProviderWithoutPlatformCode(t *testing.T) {
	rules := []online.ReferenceRule{{IDPattern: `^album:([a-z_0-9]+)$`, URLTemplate: "https://catalog.example.org/releases/${1}"}}
	policy := online.ResourcePolicy{Albums: rules, ImageHosts: []string{"*.images.example.org"}}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	cover := "https://cdn.images.example.org/cover.jpg"
	album := online.Album{ExternalID: "album:indie_42", Title: "Independent", Artists: []string{}, SourceURL: "https://catalog.example.org/releases/indie_42", CoverURL: &cover}
	if err := online.ValidateAlbum(policy, album); err != nil {
		t.Fatal("plugin-owned album declaration rejected", err)
	}
	for _, forged := range []string{"https://catalog.example.org.evil.invalid/releases/indie_42", "https://catalog.example.org/releases/other", "http://127.0.0.1/private"} {
		album.SourceURL = forged
		if online.ValidateAlbum(policy, album) == nil {
			t.Fatal("forged source accepted", forged)
		}
	}
	album.SourceURL = "https://catalog.example.org/releases/indie_42"
	cover = "https://images.example.org.evil.invalid/cover.jpg"
	if online.ValidateAlbum(policy, album) == nil {
		t.Fatal("undeclared image host accepted")
	}
}

func TestDeclaredResourcesRejectUnsafeAndUnboundedRules(t *testing.T) {
	for _, rule := range []online.ReferenceRule{
		{IDPattern: `(.+)`, URLTemplate: "https://example.org/${1}"},
		{IDPattern: `^(.+)$`, URLTemplate: "https://${1}/path"},
		{IDPattern: `^(.+)$`, URLTemplate: "http://127.0.0.1/${1}"},
		{IDPattern: `^(.+)$`, URLTemplate: "https://example.org/${9}"},
		{IDPattern: `^(.+)$`, URLTemplate: "https://example.org/constant"},
	} {
		if (online.ResourcePolicy{Tracks: []online.ReferenceRule{rule}}).Validate() == nil {
			t.Fatal("unsafe declaration accepted", rule)
		}
	}
}

func TestDeclaredImageHostsKeepURLSafetyBoundary(t *testing.T) {
	policy := online.ResourcePolicy{ImageHosts: []string{"*.images.example.org"}}
	for _, raw := range []string{
		"https://cdn.images.example.org/cover.jpg?", "https://cdn.images.example.org/cover.jpg?token=secret",
		"https://cdn.images.example.org/cover.jpg#secret", "https://user@cdn.images.example.org/cover.jpg",
		"https://cdn.images.example.org:443/cover.jpg", "https://cdn.images.example.org/",
		"https://cdn.images.example.org.evil.invalid/cover.jpg",
	} {
		if _, err := policy.ImageURL(raw); err == nil {
			t.Fatal("unsafe image reference accepted", raw)
		}
	}
}
