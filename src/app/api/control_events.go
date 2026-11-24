package api

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

type controlEvent struct {
	Type  string `json:"type"`
	Event string `json:"event"`
	Data  any    `json:"data"`
}

func (c *controlConnection) eventLoop() {
	var updates <-chan plugincontrol.Event
	if manager := c.server.control.plugins; manager != nil {
		events, unsubscribe := manager.Subscribe()
		defer unsubscribe()
		updates = events
	}
	manager := c.server.control.lifecycle
	if lifecycleLoopback(c.request) != nil {
		manager = nil
	}
	var previous []byte
	publishLifecycle := func() {
		if manager == nil {
			return
		}
		status := manager.Status()
		encoded, _ := json.Marshal(status)
		if !bytes.Equal(previous, encoded) {
			previous = encoded
			c.send(controlEvent{Type: "event", Event: "lifecycle.changed", Data: status})
		}
	}
	publishLifecycle()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	authTick := time.NewTicker(10 * time.Second)
	defer authTick.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case event := <-updates:
			c.send(controlEvent{Type: "event", Event: "capabilities.changed", Data: event})
		case <-tick.C:
			publishLifecycle()
			if !c.publishActivity() || !c.publishDownloads() {
				c.cancel()
				return
			}
		case <-authTick.C:
			if !c.authorized() {
				c.cancel()
				return
			}
		}
	}
}
