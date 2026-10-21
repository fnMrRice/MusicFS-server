package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func TestSDKAndHostErrorsHaveIdenticalTypes(t *testing.T) {
	for _, err := range []error{domain.Fail("NOT_FOUND", "Missing track"), fault.Fail("NOT_FOUND", "Missing track")} {
		var host *domain.Error
		var sdk *fault.Error
		wrapped := fmt.Errorf("request: %w", err)
		if !errors.As(wrapped, &host) || !errors.As(wrapped, &sdk) || host != sdk {
			t.Fatal("SDK and host error aliases no longer preserve errors.As identity")
		}
	}
}
