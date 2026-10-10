package pluginhost

import (
	"context"
	"os"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/src/accountstore"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func (w *OnlineWorker) initializeAccount(ctx context.Context) error {
	store, err := accountstore.Open(filepath.Join(w.config.StateDir, "credentials"), w.config.Platform+"\x00"+w.config.UserID+"\x00"+w.config.InstanceID)
	if err != nil {
		return domain.Fail("TEMPORARY_FAILURE", "Platform credential store is unavailable")
	}
	w.accountStore = store
	secret, err := store.Load()
	if err != nil {
		return domain.Fail("TEMPORARY_FAILURE", "Platform credential store cannot be read")
	}
	if len(secret) > 0 {
		_, err = w.accountCall(ctx, online.AccountRequest{Action: "restore", Secret: secret})
	}
	return err
}

func (w *OnlineWorker) accountCall(ctx context.Context, request online.AccountRequest) (online.AccountResult, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.AccountResult{}, err
	}
	return onlineCall[online.AccountResult](ctx, w, "provider.online.account", scope, pluginproto.OnlineAccount{Context: scope, Request: request})
}

// PlatformAccount strips private state before returning to the HTTP service.
func (w *OnlineWorker) PlatformAccount(ctx context.Context, request online.AccountRequest) (online.AccountView, error) {
	if request.Action == "cancel" || request.Action == "logout" {
		id := request.LoginID
		if request.Action == "logout" {
			id = "*"
		}
		if _, err := w.accountCall(ctx, online.AccountRequest{Action: "signalCancel", LoginID: id}); err != nil {
			return online.AccountView{}, err
		}
	}
	w.accountMu.Lock()
	defer w.accountMu.Unlock()
	if request.Action == "restore" || request.Action == "signalCancel" || len(request.Secret) > 0 || request.DeveloperToken != "" {
		return online.AccountView{}, domain.Fail("ACCESS_DENIED", "Private platform operations are Host-only")
	}
	if request.Action == "begin" && w.config.Platform == online.AppleMusic {
		request.DeveloperToken = os.Getenv("MUSICFS_APPLE_DEVELOPER_TOKEN")
	}
	if request.Action == "logout" {
		if err := w.accountStore.Save(nil); err != nil {
			return online.AccountView{}, domain.Fail("TEMPORARY_FAILURE", "Platform account could not be removed")
		}
	}
	result, err := w.accountCall(ctx, request)
	if err != nil {
		return online.AccountView{}, err
	}
	if err = w.accountStore.Save(result.Secret); err != nil {
		_, _ = w.accountCall(context.WithoutCancel(ctx), online.AccountRequest{Action: "logout"})
		_ = w.accountStore.Save(nil)
		return online.AccountView{}, domain.Fail("TEMPORARY_FAILURE", "Platform account could not be persisted")
	}
	return result.View, nil
}
func (w *OnlineWorker) PlatformDeveloperToken() string {
	if w.config.Platform != online.AppleMusic {
		return ""
	}
	return os.Getenv("MUSICFS_APPLE_DEVELOPER_TOKEN")
}
