package library

import (
	"errors"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

// Worker error messages are untrusted. Retain only structured retry metadata
// and host-owned public text; ordinary transport failures are not retried.
func scanFailure(err error) error {
	detail := &domain.Error{Code: pluginproto.ErrorCode(err), Message: "Provider scan failed; previous library retained"}
	var rpc *pluginproto.RPCError
	if !errors.As(err, &rpc) || !rpc.Data.Retryable {
		return detail
	}
	if rpc.Data.RetryAfterMs != nil {
		ms := *rpc.Data.RetryAfterMs
		seconds := 301
		if ms < 0 {
			seconds = -1
		} else if ms <= 300000 {
			seconds = int((ms + 999) / 1000)
		}
		detail.RetryAfter = &seconds
	}
	return jobs.Retryable(detail)
}
