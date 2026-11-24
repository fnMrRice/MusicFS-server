package api

import (
	"maps"
	"slices"
)

// NotifyScan 在扫描记录提交后标记受影响的视图。只有元数据刷新扫描进入任务队列。
func (s *Server) NotifyScan(owner, jobID string, refreshMetadata bool) {
	if refreshMetadata {
		s.NotifyRefreshTask(owner, jobID)
	} else {
		s.notifyActivity(owner, "scans")
	}
}

// NotifyRefreshTask 同时更新扫描详情和任务队列，包括扫描结束后的元数据阶段。
func (s *Server) NotifyRefreshTask(owner, taskID string) {
	s.notifyActivity(owner, "scans")
	s.control.mu.Lock()
	defer s.control.mu.Unlock()
	for conn := range s.control.connections {
		if view := conn.taskView; view != nil && (view.owner == "" || view.owner == owner) {
			view.changed(taskID)
			conn.wakeTaskSnapshot()
		}
	}
}

func (s *Server) NotifyPluginJobs(owner string) { s.notifyActivity(owner, "pluginJobs") }

func (s *Server) NotifyPlaylistSync(owner string) { s.notifyActivity(owner, "playlistSync") }

// 每个连接只累计发生变化的资源种类。写入方无需等待客户端读取或查询数据库。
func (s *Server) notifyActivity(owner, resource string) {
	s.control.mu.Lock()
	defer s.control.mu.Unlock()
	for conn := range s.control.connections {
		if conn.session.User.ID == owner {
			conn.activity[resource] = struct{}{}
		}
	}
}

func (c *controlConnection) publishActivity() bool {
	c.server.control.mu.Lock()
	resources := slices.Sorted(maps.Keys(c.activity))
	clear(c.activity)
	c.server.control.mu.Unlock()
	if len(resources) == 0 {
		return true
	}
	if !c.authorized() {
		return false
	}
	c.send(controlEvent{Type: "event", Event: "activity.changed", Data: struct {
		Resources []string `json:"resources"`
	}{resources}})
	return true
}
