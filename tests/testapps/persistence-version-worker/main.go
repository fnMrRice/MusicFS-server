package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func main() {
	peer := pluginproto.NewPeer(context.Background(), os.Stdin, os.Stdout, "w-", func(_ context.Context, _ string, raw json.RawMessage) (any, error) {
		var request map[string]any
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, err
		}
		response := map[string]any{"Protocol": request["Protocol"], "Contract": request["Contract"], "Nonce": request["Nonce"]}
		switch os.Args[1] {
		case "blank":
			response["pluginVersion"] = " "
		case "invalid":
			response["pluginVersion"] = "1.0"
		case "leading-zero":
			response["pluginVersion"] = "1.0.0-01"
		case "oversize":
			response["pluginVersion"] = "1.0.0+" + strings.Repeat("a", 123)
		}
		return response, nil
	})
	<-peer.Done()
}
