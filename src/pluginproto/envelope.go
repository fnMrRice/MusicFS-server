package pluginproto

import (
	"encoding/json"
	"errors"
	"strings"
)

type Envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type ErrorData struct {
	Code         string `json:"code"`
	Retryable    bool   `json:"retryable"`
	RetryAfterMs *int64 `json:"retryAfterMs,omitempty"`
}

type RPCError struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	Data    ErrorData `json:"data"`
}

func (e *RPCError) Error() string { return e.Data.Code + ": " + e.Message }

func Failure(code, message string) *RPCError {
	return &RPCError{Code: -32000, Message: message, Data: ErrorData{Code: code, Retryable: code == "TEMPORARY_FAILURE" || code == "RATE_LIMITED"}}
}

func ErrorCode(err error) string {
	var rpc *RPCError
	if errors.As(err, &rpc) {
		return rpc.Data.Code
	}
	if err != nil {
		for _, code := range []string{"ACCESS_DENIED", "SOURCE_OFFLINE", "RESOURCE_CHANGED", "NOT_FOUND", "UNSUPPORTED", "CANCELED", "PROTOCOL_MISMATCH"} {
			if strings.HasPrefix(err.Error(), code+":") {
				return code
			}
		}
	}
	return "TEMPORARY_FAILURE"
}
