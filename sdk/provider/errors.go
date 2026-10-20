package provider

import (
	"context"
	"errors"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func rpcError(err error) *pluginproto.RPCError {
	var rpc *pluginproto.RPCError
	if errors.As(err, &rpc) {
		return rpc
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return pluginproto.Failure("CANCELED", "Online request was canceled")
	}
	var failure *fault.Error
	if errors.As(err, &failure) {
		result := pluginproto.Failure(failure.Code, failure.Message)
		if failure.RetryAfter != nil && *failure.RetryAfter > 0 && *failure.RetryAfter <= 3600 {
			ms := int64(*failure.RetryAfter) * 1000
			result.Data.RetryAfterMs = &ms
		}
		return result
	}
	return pluginproto.Failure("TEMPORARY_FAILURE", "Online request failed")
}
