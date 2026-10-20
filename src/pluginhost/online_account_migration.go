package pluginhost

import (
	"encoding/json"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/accountstore"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// loadAccount upgrades only credentials from this instance's proven legacy
// directory. AEAD still authenticates the original platform/user/instance scope.
func (w *OnlineWorker) loadAccount(store *accountstore.Store) ([]byte, error) {
	secret, err := store.Load()
	if err == nil {
		return secret, nil
	}
	legacyID := filepath.Base(w.config.StateDir)
	if legacyID == w.config.InstanceID || !instanceid.Valid(w.config.InstanceID) || instanceid.Canonical(legacyID) != w.config.InstanceID {
		return nil, err
	}
	legacy, openErr := accountstore.Open(filepath.Join(w.config.StateDir, "credentials"), w.config.Platform+"\x00"+w.config.UserID+"\x00"+legacyID)
	if openErr != nil {
		return nil, openErr
	}
	secret, err = legacy.Load()
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(secret, &fields) != nil {
		return nil, domain.Fail("ACCESS_DENIED", "Stored platform account scope is invalid")
	}
	for key, want := range map[string]string{"platform": w.config.Platform, "userId": w.config.UserID, "instanceId": legacyID} {
		var actual string
		if json.Unmarshal(fields[key], &actual) != nil || actual != want {
			return nil, domain.Fail("ACCESS_DENIED", "Stored platform account scope is invalid")
		}
	}
	fields["instanceId"], _ = json.Marshal(w.config.InstanceID)
	secret, err = json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err = store.Save(secret); err != nil {
		return nil, err
	}
	return secret, nil
}
