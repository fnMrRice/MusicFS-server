package api

import "github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"

func (s *Server) registerPluginEvents(manager *plugincontrol.Manager) {
	s.control.plugins = manager
}
