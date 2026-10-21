package pluginrpc_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/pluginrpc"
)

type testContract interface {
	Echo(context.Context, jobs.Job, error) (jobs.Job, error)
	Guard(context.Context, mediastore.Guard) (string, error)
	Bytes(context.Context, []byte) ([]byte, error)
	Wait(context.Context) error
	State(context.Context) string
	Replay(context.Context, func() error) error
}

type testService struct{}

func (testService) Echo(_ context.Context, job jobs.Job, err error) (jobs.Job, error) {
	return job, err
}
func (testService) Guard(_ context.Context, guard mediastore.Guard) (string, error) {
	if guard == nil {
		return "", domain.Fail("INVALID_ARGUMENT", "Guard is required")
	}
	value := ""
	err := guard(func() error { value = "published"; return nil })
	return value, err
}
func (testService) Bytes(_ context.Context, bytes []byte) ([]byte, error) { return bytes, nil }
func (testService) Wait(ctx context.Context) error                        { <-ctx.Done(); return ctx.Err() }
func (testService) State(context.Context) string                          { return "complete" }

func (testService) Replay(_ context.Context, callback func() error) error {
	if err := callback(); err != nil {
		return err
	}
	return callback()
}

func endpointPair(t *testing.T) (*pluginrpc.Endpoint, *pluginrpc.Endpoint) {
	t.Helper()
	left, right := net.Pipe()
	client, server := pluginrpc.NewEndpoint(), pluginrpc.NewEndpoint()
	contract := reflect.TypeFor[testContract]()
	if err := client.SetContract(contract); err != nil {
		t.Fatal(err)
	}
	if err := server.Bind(testService{}, contract); err != nil {
		t.Fatal(err)
	}
	client.Peer = pluginproto.NewPeerWithLimit(context.Background(), left, left, "h-", client.Handle, pluginrpc.MaxMessageBytes)
	server.Peer = pluginproto.NewPeerWithLimit(context.Background(), right, right, "w-", server.Handle, pluginrpc.MaxMessageBytes)
	t.Cleanup(func() { client.Peer.Close(); server.Peer.Close() })
	return client, server
}

func TestTypedArgumentsInternalFieldsAndErrorSentinels(t *testing.T) {
	client, _ := endpointPair(t)
	job := jobs.Job{ID: "job", Owner: "private-owner", Generation: 123, PlanDigest: "internal-plan", Kind: "scan"}
	retry := 7
	for _, wantErr := range []error{
		nil, sql.ErrNoRows, context.Canceled, context.DeadlineExceeded, io.EOF,
		&domain.Error{Code: "RATE_LIMITED", Message: "Bounded retry", RetryAfter: &retry},
	} {
		var got jobs.Job
		err := client.Invoke(context.Background(), "Echo", []any{job, wantErr}, []any{&got})
		if !reflect.DeepEqual(got, job) {
			t.Fatalf("internal fields or error result lost: %+v", got)
		}
		var detail *domain.Error
		if errors.As(wantErr, &detail) {
			var actual *domain.Error
			if !errors.As(err, &actual) || !reflect.DeepEqual(actual, detail) {
				t.Fatalf("domain error changed: %v", err)
			}
		} else if !errors.Is(err, wantErr) {
			t.Fatalf("sentinel changed: want %v, got %v", wantErr, err)
		}
	}
	var state string
	if err := client.Invoke(context.Background(), "State", nil, []any{&state}); err != nil || state != "complete" {
		t.Fatalf("result without an error slot failed: %q %v", state, err)
	}
	var got jobs.Job
	err := client.Invoke(context.Background(), "Echo", []any{job, errors.New("secret connection password")}, []any{&got})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("driver error leaked: %v", err)
	}
}

func TestNestedGuardCallbacksAreScopedAndCancellationKeepsChannelUsable(t *testing.T) {
	client, _ := endpointPair(t)
	calls := 0
	var retained func() error
	guard := mediastore.Guard(func(publish func() error) error {
		calls++
		retained = publish
		return publish()
	})
	var result string
	if err := client.Invoke(context.Background(), "Guard", []any{guard}, []any{&result}); err != nil || calls != 1 || result != "published" {
		t.Fatalf("nested callback: %q, calls=%d, %v", result, calls, err)
	}
	if retained == nil || retained() == nil {
		t.Fatal("callback escaped operation lifetime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := client.Invoke(ctx, "Wait", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation sentinel changed: %v", err)
	}
	if err := client.Invoke(context.Background(), "State", nil, []any{&result}); err != nil || result != "complete" {
		t.Fatalf("channel unusable after cancellation: %v", err)
	}
}

func TestPersistencePayloadCanExceedProviderControlFrame(t *testing.T) {
	client, _ := endpointPair(t)
	body := []byte(strings.Repeat("bounded-persistence-payload", 90000))
	var got []byte
	if err := client.Invoke(context.Background(), "Bytes", []any{body}, []any{&got}); err != nil || !reflect.DeepEqual(body, got) {
		t.Fatalf("large transaction payload failed: %v", err)
	}
	got = []byte("prior")
	if err := client.Invoke(context.Background(), "Bytes", []any{[]byte(nil)}, []any{&got}); err != nil || got != nil {
		t.Fatalf("nil slice changed: %v", err)
	}
}

func TestCallbackTokenCannotBeReplayed(t *testing.T) {
	client, _ := endpointPair(t)
	calls := 0
	err := client.Invoke(context.Background(), "Replay", []any{func() error { calls++; return nil }}, nil)
	if err == nil || calls != 1 {
		t.Fatalf("callback replay was accepted: calls=%d, error=%v", calls, err)
	}
}
