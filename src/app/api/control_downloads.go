package api

import "github.com/fnMrRice/MusicFS-server/src/playback/downloadjobs"

// 每个连接按任务合并最近的进度，空闲时无需读取任务存储。
type downloadChanges map[string]downloadjobs.Summary

func (s *Server) notifyDownload(owner string, job downloadjobs.Summary, invalidated bool) {
	s.control.mu.Lock()
	defer s.control.mu.Unlock()
	for c := range s.control.connections {
		if c.session.User.ID != owner {
			continue
		}
		if c.downloads == nil {
			c.downloads = make(downloadChanges)
		}
		if invalidated {
			c.downloads[job.ID] = downloadjobs.Summary{}
		} else {
			c.downloads[job.ID] = job
		}
	}
}

func (c *controlConnection) publishDownloads() bool {
	c.server.control.mu.Lock()
	changes := c.downloads
	c.downloads = nil
	c.server.control.mu.Unlock()
	if len(changes) == 0 {
		return true
	}
	if !c.authorized() {
		return false
	}
	data := struct {
		Jobs        []downloadjobs.Summary `json:"jobs"`
		Invalidated []string               `json:"invalidated"`
	}{[]downloadjobs.Summary{}, []string{}}
	for id, job := range changes {
		if job.ID == "" {
			data.Invalidated = append(data.Invalidated, id)
		} else {
			data.Jobs = append(data.Jobs, job)
		}
	}
	c.send(controlEvent{Type: "event", Event: "downloads.progress", Data: data})
	return true
}
