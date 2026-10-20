package fault_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

func TestWrappedFailurePreservesTypedError(t *testing.T) {
	err := fault.Fail("ACCESS_DENIED", "Account revoked")
	var typed *fault.Error
	if !errors.As(fmt.Errorf("provider request: %w", err), &typed) || typed.Code != "ACCESS_DENIED" || typed != err {
		t.Fatal("wrapped SDK error lost its type or identity")
	}
}
