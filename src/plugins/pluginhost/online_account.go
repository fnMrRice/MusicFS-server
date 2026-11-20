package pluginhost

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/accountstore"
)

func (w *OnlineWorker) initializeAccount(ctx context.Context) error {
	store, err := accountstore.Open(filepath.Join(w.config.StateDir, "credentials"), w.config.Platform+"\x00"+w.config.UserID+"\x00"+w.config.InstanceID)
	if err != nil {
		return domain.Fail("TEMPORARY_FAILURE", "Platform credential store is unavailable")
	}
	w.accountStore = store
	secret, err := w.loadAccount(store)
	if err != nil {
		return domain.Fail("TEMPORARY_FAILURE", "Platform credential store cannot be read")
	}
	if len(secret) > 0 {
		_, err = w.accountCall(ctx, online.AccountRequest{Action: "restore", Secret: secret})
	} else {
		// Initialize the optional display projection without making startup
		// depend on a successful display-only state read.
		_, _ = w.accountCall(ctx, online.AccountRequest{Action: "state"})
	}
	return err
}

func (w *OnlineWorker) accountCall(ctx context.Context, request online.AccountRequest) (online.AccountResult, error) {
	if request.Action == "logout" || (request.Action == "signalCancel" && request.LoginID == "*") {
		w.accountDisplay.invalidate()
	}
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.AccountResult{}, err
	}
	mutation := request.Action != "state" && request.Action != "signalCancel"
	generation := w.accountDisplay.begin(mutation)
	wire, err := onlineCall[accountResultWire](ctx, w, "provider.online.account", scope, pluginproto.OnlineAccount{Context: scope, Request: request})
	result := online.AccountResult{View: wire.View, Secret: wire.Secret, RecommendationBinding: wire.RecommendationBinding}
	if err == nil && len(wire.DisplayCount) > 0 {
		// Invalid optional statistics never invalidate an otherwise valid login.
		result.DisplayCount, _ = plugindisplay.Decode(wire.DisplayCount)
	}
	if request.Action != "signalCancel" {
		w.accountDisplay.finish(generation, mutation, result.View, result.DisplayCount, err == nil)
	}
	return result, err
}

type accountResultWire struct {
	View                  online.AccountView `json:"view"`
	Secret                json.RawMessage    `json:"secret,omitempty"`
	DisplayCount          json.RawMessage    `json:"displayCount,omitempty"`
	RecommendationBinding string             `json:"recommendationBinding,omitempty"`
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
func (w *OnlineWorker) PlatformAccountOptions(ctx context.Context) (online.AccountOptions, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.AccountOptions{}, err
	}
	options, err := onlineCall[online.AccountOptions](ctx, w, "provider.online.accountOptions", scope, pluginproto.OnlineScope{Context: scope})
	if err == nil {
		err = options.Validate(w.descriptor.Login)
	}
	return options, err
}

// PlatformSession reads worker state without exposing or rewriting its secret.
func (w *OnlineWorker) PlatformSession(ctx context.Context) (online.AccountView, error) {
	result, err := w.accountCall(ctx, online.AccountRequest{Action: "state"})
	return result.View, err
}

func (w *OnlineWorker) AccountScope(ctx context.Context) (string, string, error) {
	account, revision, _, err := w.RecommendationAccountScope(ctx)
	return account, revision, err
}

func (w *OnlineWorker) RecommendationAccountScope(ctx context.Context) (string, string, string, error) {
	if w.descriptor.Authentication == "anonymous" {
		select {
		case <-w.done:
			return "", "", "", domain.Fail("SOURCE_OFFLINE", "Online worker is offline")
		default:
			return "", "", "", ctx.Err()
		}
	}
	result, err := w.accountCall(ctx, online.AccountRequest{Action: "state"})
	if err != nil {
		return "", "", "", err
	}
	return result.RecommendationScope()
}
