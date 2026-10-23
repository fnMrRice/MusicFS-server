package plugins_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineAudioWireContractsPreserveUnknownSizeAndBoundedChunks(t *testing.T) {
	scope := pluginproto.Context{InstanceID: bilibiliContractInstance, UserID: "owner", TaskID: strings.Repeat("a", 48), TimeoutMs: 10000}
	stream := strings.Repeat("b", 48)
	audio := pluginproto.OnlineAudio{ExternalID: "audio:10", MIME: "audio/mp4", Codec: "aac", Container: "m4a"}
	for _, size := range []int64{-1, 0, 4096, pluginproto.MaxOnlineAudioBytes} {
		audio.SizeBytes = size
		if err := audio.Validate(); err != nil {
			t.Fatal(err)
		}
		onlineContract(t, "AudioDescriptor", audio)
		onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "audio-described", "result": pluginproto.OnlineResult[pluginproto.OnlineAudio]{Context: scope, Value: audio}})
		onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "audio-opened", "result": pluginproto.OnlineResult[pluginproto.OnlineAudioOpened]{Context: scope, Value: pluginproto.OnlineAudioOpened{Audio: audio, StreamID: stream}}})
	}
	for _, request := range []struct {
		method string
		params any
	}{
		{"audioDescribe", pluginproto.OnlineReference{Context: scope, ExternalID: audio.ExternalID}},
		{"audioOpen", pluginproto.OnlineReference{Context: scope, ExternalID: audio.ExternalID}},
		{"audioRead", pluginproto.OnlineAudioRead{Context: scope, StreamID: stream, Offset: 0}},
		{"audioClose", pluginproto.OnlineAudioRead{Context: scope, StreamID: stream, Offset: 65536}},
	} {
		onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "audio-request", "method": "provider.online." + request.method, "params": request.params})
	}
	for _, chunk := range []pluginproto.OnlineAudioChunk{{Data: make([]byte, pluginproto.OnlineAudioChunkBytes)}, {Offset: 65536, Data: []byte{}, EOF: true}, {EOF: true}} {
		onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "audio-chunk", "result": pluginproto.OnlineResult[pluginproto.OnlineAudioChunk]{Context: scope, Value: chunk}})
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["sizeBytes"] = -2 },
		func(v map[string]any) { v["sizeBytes"] = pluginproto.MaxOnlineAudioBytes + 1 },
		func(v map[string]any) { v["mime"] = "video/mp4" },
		func(v map[string]any) { v["codec"] = "mp3" },
		func(v map[string]any) { v["url"] = "https://audio.example.invalid/private" },
		func(v map[string]any) { v["cookie"] = "private" },
	} {
		body, _ := json.Marshal(audio)
		value := testkit.DecodeJSON(t, body).(map[string]any)
		mutate(value)
		if onlineSchema(t, "AudioDescriptor").Validate(value) == nil {
			t.Fatal("invalid online audio descriptor accepted", value)
		}
	}
	for _, extra := range []int{1, 2, 3} {
		oversized, _ := json.Marshal(pluginproto.OnlineAudioChunk{Data: make([]byte, pluginproto.OnlineAudioChunkBytes+extra)})
		if onlineSchema(t, "AudioChunk").Validate(testkit.DecodeJSON(t, oversized)) == nil {
			t.Fatal("oversized online audio chunk accepted", extra)
		}
	}
}
