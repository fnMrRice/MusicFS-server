package playback_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/playback/downloadjobs"
)

func TestDownloadJobResolvesNewSourcesAndStoresActualVersions(t *testing.T) {
	f := newNetworkPlaybackFixture(t, true)
	res := f.request(t, http.MethodPost, "/api/v1/online-instances/network-fixture/download-jobs", `{"externalIds":["audio:2","audio:2","audio:1"],"quality":"lowest","accountId":""}`, map[string]string{"Idempotency-Key": "network-batch-once"})
	var job downloadjobs.Job
	if res.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("submit: %s %s", res.Status, body)
	}
	err := json.NewDecoder(res.Body).Decode(&job)
	res.Body.Close()
	if err != nil || job.Total != 2 {
		t.Fatal(job, err)
	}
	for deadline := time.Now().Add(5 * time.Second); job.Completed+job.Failed < 2 && time.Now().Before(deadline); {
		res = f.request(t, http.MethodGet, "/api/v1/online-download-jobs/"+job.ID, "", nil)
		err = json.NewDecoder(res.Body).Decode(&job)
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatal(res.Status, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if job.State != "succeeded" || job.Items[0].ExternalID != "audio:2" || job.Items[1].ExternalID != "audio:1" || f.provider.opens.Load() != 2 {
		t.Fatalf("unexpected publication: %+v", job)
	}
	records, err := f.store.ListOnlineDownloads(t.Context())
	if err != nil || len(records) != 2 {
		t.Fatal(records, err)
	}
	versions := map[string]bool{}
	for _, record := range records {
		versions[record.ID] = true
	}
	for _, item := range job.Items {
		if item.Version == nil || item.Version.Quality != "lowest" || !versions[item.Version.ID] {
			t.Fatalf("download receipt disagrees with stored version: %+v", item)
		}
	}
}
