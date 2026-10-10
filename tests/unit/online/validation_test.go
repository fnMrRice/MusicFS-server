package online_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
)

func TestMalformedSearchAndDetailNeverBecomeEmptySuccess(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"code":200}`, `{"code":200,"result":{}}`, `{"code":200,"result":{"songCount":-1}}`,
		`{"code":200,"result":{"songCount":1,"songs":[{"id":1.5,"name":"bad"}]}}`,
		`{"code":200,"result":{"songCount":1,"songs":[{"id":1,"name":"bad","duration":-1}]}}`,
		`{"code":200,"result":{"songCount":1,"songs":[{"id":1,"name":"bad\u0000"}]}}`,
		neSearch + `{}`, strings.Repeat(" ", (2<<20)+1),
	} {
		c := scoped(t, online.Netease, response(body))
		_, err := c.Search(context.Background(), online.SearchRequest{Query: "test", Limit: 1})
		expectCode(t, err, "TEMPORARY_FAILURE")
	}
	for _, body := range []string{`{"code":200,"songs":[{"id":43,"name":"wrong"}]}`, `{"code":200,"songs":[{"id":42,"name":"test","dt":999999999999}]}`} {
		c := scoped(t, online.Netease, response(body))
		_, err := c.Metadata(context.Background(), "42")
		expectCode(t, err, "TEMPORARY_FAILURE")
	}
}

func TestStatusMappingAndRetryAfterWithoutUpstreamTextLeak(t *testing.T) {
	for status, code := range map[int]string{401: "AUTH_REQUIRED", 403: "ACCESS_DENIED", 404: "NOT_FOUND", 429: "RATE_LIMITED", 500: "TEMPORARY_FAILURE", 302: "TEMPORARY_FAILURE"} {
		c := scoped(t, online.Netease, roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"17"}}, Body: io.NopCloser(strings.NewReader("private-upstream-detail"))}, nil
		}))
		_, err := c.Search(context.Background(), online.SearchRequest{Query: "test", Limit: 1})
		expectCode(t, err, code)
		if strings.Contains(err.Error(), "private-upstream-detail") {
			t.Fatal("upstream body leaked")
		}
		if status == 429 {
			_, err = c.Search(context.Background(), online.SearchRequest{Query: "test", Limit: 1})
			expectCode(t, err, "RATE_LIMITED")
			if err.(*domain.Error).RetryAfter == nil || *err.(*domain.Error).RetryAfter < 16 {
				t.Fatal("upstream cooldown was ignored")
			}
		}
	}
}

func TestPublicPlaylistPreservesOccurrencesAndCompleteness(t *testing.T) {
	for _, count := range []string{"3", "4"} {
		c := scoped(t, online.Netease, response(`{"code":200,"playlist":{"id":91,"name":"Synthetic playlist","privacy":0,"trackCount":`+count+`,"trackIds":[{"id":42},{"id":42},{"id":"9007199254740993"}],"tracks":[{"id":42}],"updateTime":123}}`))
		observation, err := c.ObservePlaylist(context.Background(), "91")
		if err != nil {
			t.Fatal(err)
		}
		if len(observation.Entries) != 3 || observation.Entries[0].ExternalTrackID != observation.Entries[1].ExternalTrackID || observation.Entries[1].Position != 1 || observation.Complete != (count == "3") || observation.Consistency != online.Unversioned || len(observation.MetadataDigest) != 64 {
			t.Fatalf("bad observation: %+v", observation)
		}
	}
	for _, body := range []string{
		`{"code":200,"playlist":{"id":91,"name":"Synthetic","trackCount":0,"trackIds":[]}}`,
		`{"code":200,"playlist":{"id":91,"name":"Synthetic","privacy":10,"trackCount":0,"trackIds":[]}}`,
	} {
		c := scoped(t, online.Netease, response(body))
		_, err := c.ObservePlaylist(context.Background(), "91")
		expectCode(t, err, "ACCESS_DENIED")
	}
}

func TestRecommendationsAreAnonymousOrderedAndHaveNoSyntheticPaging(t *testing.T) {
	c := scoped(t, online.Netease, response(`{"code":200,"hasTaste":false,"result":[{"id":91,"name":"Synthetic","copywriter":"Public selection","trackCount":2},{"id":92,"name":"Second","trackCount":0}]}`))
	page, err := c.PublicRecommendations(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Personalized || page.NextCursor != nil || len(page.Items) != 2 || page.Items[1].Position != 1 || !page.ExpiresAt.After(page.ObservedAt) {
		t.Fatalf("bad recommendation page: %+v", page)
	}
	qq := scoped(t, online.QQMusic, response(`{}`))
	_, err = qq.PublicRecommendations(context.Background(), 1)
	expectCode(t, err, "UNSUPPORTED")
}
