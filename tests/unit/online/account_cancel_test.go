package online_test

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"net/http"
	"testing"
	"time"
)

func TestPlatformCancelSignalsInFlightPoll(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	release := make(chan struct{})
	c := anonymousScoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/eapi/login/qrcode/unikey":
			return accountResponse(`{"code":200,"unikey":"fixture"}`), nil
		case "/eapi/login/qrcode/client/login":
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			return accountResponse(`{"code":803}`, "MUSIC_U=fixture; Secure"), nil
		default:
			return accountResponse(`{"code":200,"profile":{"userId":12345,"nickname":"Fixture"}}`), nil
		}
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	begin, err := c.Account(ctx, online.AccountRequest{Action: "begin"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(3100 * time.Millisecond)
	result := make(chan online.AccountResult, 1)
	failure := make(chan error, 1)
	go func() {
		v, e := c.Account(ctx, online.AccountRequest{Action: "poll", LoginID: begin.View.LoginID})
		result <- v
		failure <- e
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("poll did not start")
	}
	if _, err = c.Account(ctx, online.AccountRequest{Action: "signalCancel", LoginID: begin.View.LoginID}); err != nil {
		t.Fatal(err)
	}
	close(release)
	v := <-result
	if err = <-failure; err != nil {
		t.Fatal(err)
	}
	if v.View.State != "canceled" || len(v.Secret) > 0 {
		t.Fatal("canceled poll published an account")
	}
}
