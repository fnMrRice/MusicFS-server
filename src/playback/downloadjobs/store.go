package downloadjobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

// 每次只原子替换一个小批次，下载一首歌不需要重写整张歌单的结果。
const blockSize = 64

var jobName = regexp.MustCompile(`^[0-9a-f]{48}$`)

type progress struct {
	At    time.Time
	Items []Item
}

func atomicJSON(path string, value any) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".checkpoint-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(value); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (s *Service) saveNew(e *entry) error {
	dir := filepath.Join(s.dir, e.ID)
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	if err := atomicJSON(filepath.Join(dir, "request.json"), e.receipt); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	return nil
}
func (s *Service) record(e *entry, items []Item) error {
	start := e.next()
	block := start / blockSize
	previous := e.Items[block*blockSize : start]
	at := time.Now().UTC()
	p := progress{At: at, Items: make([]Item, 0, len(previous)+len(items))}
	p.Items = append(p.Items, previous...)
	p.Items = append(p.Items, items...)
	if err := atomicJSON(filepath.Join(s.dir, e.ID, strconv.Itoa(block)+".json"), p); err != nil {
		return err
	}
	e.apply(items, at)
	s.notify(e, false)
	return nil
}
func (s *Service) restore() error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	files, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if !f.IsDir() || !jobName.MatchString(f.Name()) {
			continue
		}
		dir := filepath.Join(s.dir, f.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "request.json"))
		if errors.Is(err, os.ErrNotExist) {
			// 中断在提交回执之前的临时目录没有已接受的请求。
			if err = os.RemoveAll(dir); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		var r receipt
		if err = json.Unmarshal(raw, &r); err != nil {
			return err
		}
		if len(r.ExternalIDs) == 0 || len(r.ExternalIDs) > online.MaxPlaylistEntries || r.Operation.Owner == "" {
			return fmt.Errorf("invalid online download receipt %s", f.Name())
		}
		e := newEntry(f.Name(), r)
		for block := 0; e.next() < e.Total; block++ {
			raw, err = os.ReadFile(filepath.Join(dir, strconv.Itoa(block)+".json"))
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			if err != nil {
				return err
			}
			var p progress
			if err = json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if len(p.Items) < 1 || len(p.Items) > min(blockSize, e.Total-e.next()) {
				return fmt.Errorf("invalid online download checkpoint %s", e.ID)
			}
			for i, item := range p.Items {
				if item.ExternalID != e.Items[e.next()+i].ExternalID || !(item.State == "succeeded" && item.Version != nil && item.Error == nil || item.State == "failed" && item.Error != nil && item.Version == nil) {
					return fmt.Errorf("invalid online download item %s", e.ID)
				}
			}
			e.apply(p.Items, p.At)
			if len(p.Items) < blockSize {
				break
			}
		}
		if e.next() == e.Total && time.Since(e.updated) >= retention {
			if err = os.RemoveAll(dir); err != nil {
				return err
			}
			continue
		}
		s.jobs[e.ID] = e
		s.operations[r.Operation] = e
		s.items += e.Total
		if e.next() != e.Total {
			e.resumed = true
			s.pending = append(s.pending, e)
		}
	}
	return nil
}
