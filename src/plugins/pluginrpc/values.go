// Package pluginrpc implements the trusted Go persistence plugin transport.
// Only explicitly supplied interfaces are callable. It is not a SQL endpoint.
package pluginrpc

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

var errorType = reflect.TypeFor[error]()
var contextType = reflect.TypeFor[context.Context]()

const MaxValueBytes = 48 << 20

type wireError struct {
	Kind      string
	Detail    *domain.Error
	Retryable bool
}

type value struct {
	Nil      bool
	Data     []byte
	Callback string
	Error    *wireError
	Shape    *dataShape `json:",omitempty"`
}

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
}

func encodeError(err error) (result *wireError) {
	defer func() {
		if result != nil {
			result.Retryable = jobs.IsRetryable(err)
		}
	}()
	if err == nil {
		return nil
	}
	var detail *domain.Error
	switch {
	case errors.As(err, &detail):
		return &wireError{Kind: "domain", Detail: detail}
	case errors.Is(err, context.Canceled):
		return &wireError{Kind: "canceled"}
	case errors.Is(err, context.DeadlineExceeded):
		return &wireError{Kind: "deadline"}
	case errors.Is(err, sql.ErrNoRows):
		return &wireError{Kind: "noRows"}
	case errors.Is(err, io.EOF):
		return &wireError{Kind: "eof"}
	default:
		// Driver errors may contain paths, queries or credentials.
		return &wireError{Kind: "failure"}
	}
}

func decodeError(w *wireError) error {
	err := decodeBaseError(w)
	if w != nil && w.Retryable {
		return jobs.Retryable(err)
	}
	return err
}

func decodeBaseError(w *wireError) error {
	if w == nil {
		return nil
	}
	switch w.Kind {
	case "domain":
		if w.Detail != nil {
			return w.Detail
		}
	case "canceled":
		return context.Canceled
	case "deadline":
		return context.DeadlineExceeded
	case "noRows":
		return sql.ErrNoRows
	case "eof":
		return io.EOF
	}
	return domain.Fail("TEMPORARY_FAILURE", "Persistence operation failed; verify its result before retrying")
}

func (e *Endpoint) encode(ctx context.Context, v reflect.Value, cleanup *[]string) (value, error) {
	if v.Type() == errorType {
		if v.IsNil() {
			return value{Nil: true}, nil
		}
		return value{Error: encodeError(v.Interface().(error))}, nil
	}
	switch v.Kind() {
	case reflect.Func, reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		if v.IsNil() {
			return value{Nil: true}, nil
		}
	}
	if v.Kind() == reflect.Func {
		id := domain.NewID()
		e.mu.Lock()
		if len(e.callbacks) >= 256 {
			e.mu.Unlock()
			return value{}, fmt.Errorf("too many persistence callbacks")
		}
		e.callbacks[id] = callback{fn: v, ctx: ctx}
		e.mu.Unlock()
		*cleanup = append(*cleanup, id)
		return value{Callback: id}, nil
	}
	shape, err := captureShape(v)
	if err != nil {
		return value{}, fmt.Errorf("unsupported persistence shape")
	}
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).EncodeValue(v); err != nil {
		return value{}, fmt.Errorf("unsupported persistence value")
	}
	if buffer.Len() > MaxValueBytes {
		return value{}, fmt.Errorf("persistence value exceeds its size limit")
	}
	return value{Data: buffer.Bytes(), Shape: shape}, nil
}

func (e *Endpoint) decode(ctx context.Context, w value, typ reflect.Type) (reflect.Value, error) {
	if typ == errorType {
		if err := decodeError(w.Error); err != nil {
			return reflect.ValueOf(err).Convert(errorType), nil
		}
		return reflect.Zero(errorType), nil
	}
	if w.Nil {
		switch typ.Kind() {
		case reflect.Func, reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
			return reflect.Zero(typ), nil
		default:
			return reflect.Value{}, fmt.Errorf("unexpected nil persistence value")
		}
	}
	if typ.Kind() == reflect.Func {
		if len(w.Callback) != 48 {
			return reflect.Value{}, fmt.Errorf("invalid persistence callback")
		}
		return reflect.MakeFunc(typ, func(args []reflect.Value) []reflect.Value {
			result, err := e.call(ctx, "callback", w.Callback, args, resultTypes(typ))
			if err == nil {
				return result
			}
			result = make([]reflect.Value, typ.NumOut())
			for i := range result {
				result[i] = reflect.Zero(typ.Out(i))
			}
			if len(result) > 0 && typ.Out(len(result)-1) == errorType {
				result[len(result)-1] = reflect.ValueOf(err).Convert(errorType)
			}
			return result
		}), nil
	}
	if w.Callback != "" || w.Error != nil || len(w.Data) > MaxValueBytes {
		return reflect.Value{}, fmt.Errorf("invalid persistence value")
	}
	result := reflect.New(typ)
	if err := gob.NewDecoder(bytes.NewReader(w.Data)).DecodeValue(result); err != nil {
		return reflect.Value{}, fmt.Errorf("invalid persistence payload")
	}
	if err := restoreShape(result.Elem(), w.Shape, 0); err != nil {
		return reflect.Value{}, err
	}
	return result.Elem(), nil
}

func resultTypes(typ reflect.Type) []reflect.Type {
	result := make([]reflect.Type, typ.NumOut())
	for i := range result {
		result[i] = typ.Out(i)
	}
	return result
}
