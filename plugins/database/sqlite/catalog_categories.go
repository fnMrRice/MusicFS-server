package sqlite

import (
	"context"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
)

const maxCatalogCategoryMemberships = catalog.MaxItems * 16

type catalogCategoryIndex struct {
	items   map[string]catalog.Category
	members map[string][]string
}

// 分类身份仅取决于用户、来源和标签；同一曲目的重复署名不重复计数。
func indexCatalogCategories(ctx context.Context, owner, kind string, tracks []catalogTrack) (catalogCategoryIndex, error) {
	index := catalogCategoryIndex{items: map[string]catalog.Category{}, members: map[string][]string{}}
	bytes, memberships := 0, 0
	for _, t := range tracks {
		if err := ctx.Err(); err != nil {
			return index, err
		}
		seen := map[string]bool{}
		add := func(category catalog.Category) error {
			if seen[category.ID] {
				return nil
			}
			seen[category.ID] = true
			previous, exists := index.items[category.ID]
			memberships++
			bytes += 64
			if !exists {
				bytes += 256 + len(category.Name) + len(category.ProviderInstanceID) + len(category.RootID)
			}
			if !exists && len(index.items) == catalog.MaxItems || memberships > maxCatalogCategoryMemberships || bytes > catalog.MaxSnapshotBytes {
				return domain.Fail("RATE_LIMITED", "Catalog category index exceeds its resource budget")
			}
			if exists && kind == "genre" && previous.Name < category.Name {
				category.Name = previous.Name
			}
			category.TrackCount = previous.TrackCount + 1
			index.items[category.ID] = category
			index.members[t.ID] = append(index.members[t.ID], category.ID)
			return nil
		}
		if kind == "folder" {
			for _, d := range t.directories {
				if err := add(catalog.Category{ID: d.ID, Kind: kind, Name: d.Name, ProviderInstanceID: d.ProviderInstanceID, RootID: d.RootID, ParentID: d.ParentID}); err != nil {
					return index, err
				}
			}
			continue
		}
		if len(t.Artists) > maxCatalogCategoryMemberships || len(t.albumNames) > maxCatalogCategoryMemberships {
			return index, domain.Fail("RATE_LIMITED", "Catalog track credits exceed their resource budget")
		}
		names := catalogCategoryNames(t, kind)
		unknown := len(names) == 0
		if unknown {
			names = []string{"Unknown " + kind}
		}
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return index, err
			}
			if err := add(namedCatalogCategory(owner, kind, t.ProviderID, name, unknown)); err != nil {
				return index, err
			}
		}
	}
	return index, nil
}

func catalogCategoryNames(t catalogTrack, kind string) []string {
	var names []string
	switch kind {
	case "artist":
		names = t.Artists
	case "album":
		if strings.TrimSpace(catalogText(t.Album)) != "" {
			names = []string{*t.Album}
		} else {
			names = t.albumNames
		}
	case "genre":
		names = []string{strings.TrimSpace(catalogText(t.Genre))}
	}
	return slices.DeleteFunc(slices.Clone(names), func(name string) bool { return strings.TrimSpace(name) == "" })
}

func namedCatalogCategory(owner, kind, provider, name string, unknown bool) catalog.Category {
	if kind == "genre" {
		provider = ""
	}
	value, identityKind := name, "category:"+kind
	if kind == "genre" {
		value = strings.ToLower(name)
	}
	if unknown {
		value, identityKind = "", identityKind+":unknown"
	}
	id := catalogID(owner, provider, identityKind, value)
	return catalog.Category{ID: id, Kind: kind, Name: name, Unknown: unknown, ProviderInstanceID: provider}
}

func prepareCatalogCategories(ctx context.Context, owner string, data catalogData, q catalog.Query) (catalogCategoryIndex, error) {
	index, err := indexCatalogCategories(ctx, owner, q.CategoryKind, data.tracks)
	if err != nil {
		return index, err
	}
	for _, id := range []string{q.CategoryID, q.ParentID} {
		if id != "" {
			if _, exists := index.items[id]; !exists {
				return index, domain.Fail("NOT_FOUND", "Catalog category not found")
			}
		}
	}
	return index, nil
}

func catalogCategoryResults(ctx context.Context, index catalogCategoryIndex, tracks []catalogTrack, q catalog.Query) ([]catalogResult, error) {
	counts := map[string]int{}
	for _, t := range tracks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, id := range index.members[t.ID] {
			counts[id]++
		}
	}
	items := []catalogResult{}
	breadcrumbBytes := 0
	for id, count := range counts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		category := index.items[id]
		if q.CategoryID != "" {
			if id != q.CategoryID {
				continue
			}
		} else if q.CategoryKind == "folder" {
			if q.ParentID == "" && category.ParentID != nil || q.ParentID != "" && (category.ParentID == nil || *category.ParentID != q.ParentID) {
				continue
			}
		}
		if !catalogMatches(category.Name, q.Q) {
			continue
		}
		category.TrackCount = count
		// 只为去重后且实际返回的分类生成面包屑，避免按每首曲目重复展开祖先链。
		for ancestor := category; ; {
			breadcrumbBytes += 64 + len(ancestor.Name)
			if breadcrumbBytes > catalog.MaxSnapshotBytes || len(category.Breadcrumbs) > maxCatalogDirectoryDepth {
				return nil, domain.Fail("RATE_LIMITED", "Catalog breadcrumbs exceed their resource budget")
			}
			category.Breadcrumbs = append(category.Breadcrumbs, catalog.CategoryRef{ID: ancestor.ID, Name: ancestor.Name})
			if ancestor.ParentID == nil {
				break
			}
			ancestor = index.items[*ancestor.ParentID]
		}
		slices.Reverse(category.Breadcrumbs)
		items = append(items, catalogResult{id: id, name: category.Name, data: category})
	}
	return items, nil
}
