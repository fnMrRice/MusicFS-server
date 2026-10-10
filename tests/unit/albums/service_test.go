package albums_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type albumRepository struct {
	albums.Repository
	created int
}

func (r *albumRepository) CreateSourceAlbum(_ context.Context, owner string, request albums.CreateSourceAlbumRequest, _ string) (albums.SourceAlbum, error) {
	r.created++
	return albums.SourceAlbum{Owner: owner, ProviderInstanceID: request.ProviderInstanceID, SourceTrackIDs: []string{}}, nil
}

type providerAuthorization func(context.Context, string, string) error

func (f providerAuthorization) AuthorizeProvider(ctx context.Context, owner, provider string) error {
	return f(ctx, owner, provider)
}

func TestAlbumCreationRequiresConfiguredProviderEvenWhenEmpty(t *testing.T) {
	ctx := context.Background()
	repo := &albumRepository{}
	request := albums.CreateSourceAlbumRequest{ProviderInstanceID: "offline-provider", Name: "Empty album", SourceTrackIDs: []string{}}
	if _, err := albums.New(repo, nil, nil).CreateSourceAlbum(ctx, "owner", request, "create-key"); err == nil || repo.created != 0 {
		t.Fatal("missing provider authorizer allowed creation")
	}
	denied := domain.Fail("NOT_FOUND", "Provider not found")
	service := albums.New(repo, nil, providerAuthorization(func(_ context.Context, owner, provider string) error {
		if owner != "owner" || provider != "offline-provider" {
			return denied
		}
		return nil
	}))
	if _, err := service.CreateSourceAlbum(ctx, "other-owner", request, "denied-key"); !errors.Is(err, denied) || repo.created != 0 {
		t.Fatal("unauthorized provider reached storage", err)
	}
	created, err := service.CreateSourceAlbum(ctx, "owner", request, "create-key")
	if err != nil || repo.created != 1 || created.ProviderInstanceID != "offline-provider" {
		t.Fatal("configured offline provider cannot own an empty album", err)
	}
}
