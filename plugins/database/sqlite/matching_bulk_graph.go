package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/library"
)

type bulkMergeSource struct {
	track     library.Track
	recording string
	unlinked  bool
	current   bool
}

// 先保留现有录音的完整成员，再把本次命中的关系加入连通分组。
type bulkMergeGraph struct {
	parents map[string]string
	sizes   map[string]int
	touched map[string]bool
}

func newBulkMergeGraph(sources map[string]bulkMergeSource) *bulkMergeGraph {
	g := &bulkMergeGraph{parents: map[string]string{}, sizes: map[string]int{}, touched: map[string]bool{}}
	recordings := map[string]string{}
	for id := range sources {
		g.parents[id], g.sizes[id] = id, 1
	}
	for id, source := range sources {
		if source.recording == "" {
			continue
		}
		if previous := recordings[source.recording]; previous != "" {
			g.join(id, previous)
		} else {
			recordings[source.recording] = id
		}
	}
	return g
}

func (g *bulkMergeGraph) root(id string) string {
	parent := g.parents[id]
	if parent != id {
		g.parents[id] = g.root(parent)
	}
	return g.parents[id]
}

func (g *bulkMergeGraph) join(left, right string) {
	l, r := g.root(left), g.root(right)
	if l == r {
		return
	}
	if g.sizes[l] < g.sizes[r] {
		l, r = r, l
	}
	g.parents[r] = l
	g.sizes[l] += g.sizes[r]
}

func (g *bulkMergeGraph) match(left, right string) {
	g.touched[left], g.touched[right] = true, true
	g.join(left, right)
}

func (g *bulkMergeGraph) groups() [][]string {
	active := map[string]bool{}
	for id := range g.touched {
		active[g.root(id)] = true
	}
	members := map[string][]string{}
	for id := range g.parents {
		root := g.root(id)
		if active[root] {
			members[root] = append(members[root], id)
		}
	}
	groups := make([][]string, 0, len(members))
	for _, ids := range members {
		sort.Strings(ids)
		groups = append(groups, ids)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i][0] < groups[j][0] })
	return groups
}

func readBulkMergeSources(ctx context.Context, tx *sql.Tx, owner string) (map[string]bulkMergeSource, error) {
	rows, err := tx.QueryContext(ctx, `SELECT t.id,t.document,COALESCE(l.recording_id,''),COALESCE(l.decision,''),
 COALESCE(a.current=1 AND a.version=t.version AND a.track_id=t.id,0)
 FROM tracks t LEFT JOIN recording_source_links l ON l.owner=t.owner AND l.source_id=t.id
 LEFT JOIN source_assets a ON a.owner=t.owner AND a.id=t.asset_id WHERE t.owner=? ORDER BY t.id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := map[string]bulkMergeSource{}
	for rows.Next() {
		var id, decision string
		var raw []byte
		var source bulkMergeSource
		if err = rows.Scan(&id, &raw, &source.recording, &decision, &source.current); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &source.track); err != nil {
			return nil, err
		}
		source.unlinked = decision == "unlinked"
		source.current = source.current && source.track.Availability == "available"
		sources[id] = source
	}
	return sources, rows.Err()
}

func bulkMergeLabel(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// 作者必须全部非空且完整集合相同；保留括号和版本说明，不按作者交集匹配。
func bulkMergeIdentity(track library.Track) string {
	if track.Title == nil || len(track.Artists) == 0 {
		return ""
	}
	title := bulkMergeLabel(*track.Title)
	if title == "" {
		return ""
	}
	unique := map[string]bool{}
	for _, artist := range track.Artists {
		name := bulkMergeLabel(artist)
		if name == "" {
			return ""
		}
		unique[name] = true
	}
	artists := make([]string, 0, len(unique))
	for artist := range unique {
		artists = append(artists, artist)
	}
	sort.Strings(artists)
	value, _ := json.Marshal(struct {
		Title   string
		Artists []string
	}{title, artists})
	return string(value)
}

func addBulkMergeIdentities(g *bulkMergeGraph, sources map[string]bulkMergeSource) {
	first := map[string]string{}
	for id, source := range sources {
		if !source.current || source.unlinked {
			continue
		}
		identity := bulkMergeIdentity(source.track)
		if identity == "" {
			continue
		}
		if previous := first[identity]; previous != "" {
			g.match(previous, id)
		} else {
			first[identity] = id
		}
	}
}
