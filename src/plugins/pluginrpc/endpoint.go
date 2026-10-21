package pluginrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type callback struct {
	fn  reflect.Value
	ctx context.Context
}

type Endpoint struct {
	Peer      *pluginproto.Peer
	mu        sync.Mutex
	methods   map[string]reflect.Value
	contracts map[string]reflect.Type
	callbacks map[string]callback
}

type request struct {
	Name   string
	Values []value
}

type response struct {
	Values []value
}

func NewEndpoint() *Endpoint {
	return &Endpoint{methods: map[string]reflect.Value{}, contracts: map[string]reflect.Type{}, callbacks: map[string]callback{}}
}

func (e *Endpoint) SetContract(contract reflect.Type) error {
	if contract == nil || contract.Kind() != reflect.Interface {
		return fmt.Errorf("invalid persistence contract")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := 0; i < contract.NumMethod(); i++ {
		method := contract.Method(i)
		e.contracts[method.Name] = method.Type
	}
	return nil
}

// Bind exposes only the named interface, including its inherited methods.
func (e *Endpoint) Bind(service any, contract reflect.Type) error {
	object := reflect.ValueOf(service)
	if contract == nil || contract.Kind() != reflect.Interface || !object.IsValid() ||
		(object.Kind() == reflect.Pointer && object.IsNil()) || !object.Type().Implements(contract) {
		return fmt.Errorf("persistence implementation does not satisfy its contract")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := 0; i < contract.NumMethod(); i++ {
		method := contract.Method(i)
		if method.Name == "Close" {
			continue
		}
		e.methods[method.Name] = object.MethodByName(method.Name)
	}
	return nil
}

func (e *Endpoint) release(ids []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, id := range ids {
		delete(e.callbacks, id)
	}
}

func (e *Endpoint) Handle(ctx context.Context, method string, raw json.RawMessage) (result any, err error) {
	defer func() {
		if recover() != nil {
			result, err = nil, domain.Fail("TEMPORARY_FAILURE", "Persistence plugin operation failed")
		}
	}()
	var input request
	if json.Unmarshal(raw, &input) != nil || len(input.Values) > 32 {
		return nil, fmt.Errorf("invalid persistence request")
	}
	e.mu.Lock()
	fn, ok := e.methods[input.Name]
	var callbackContext context.Context
	if method == "callback" {
		cb, exists := e.callbacks[input.Name]
		delete(e.callbacks, input.Name)
		fn, ok = cb.fn, exists
		callbackContext = cb.ctx
		if exists && cb.ctx.Err() != nil {
			ok = false
		}
	} else if method != "invoke" {
		ok = false
	}
	e.mu.Unlock()
	if !ok {
		return nil, domain.Fail("UNSUPPORTED", "Persistence method or callback is unavailable")
	}
	if callbackContext != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		stop := context.AfterFunc(callbackContext, cancel)
		defer stop()
		defer cancel()
	}
	typ := fn.Type()
	offset := 0
	args := make([]reflect.Value, typ.NumIn())
	if typ.NumIn() > 0 && typ.In(0) == contextType {
		args[0], offset = reflect.ValueOf(ctx), 1
	}
	if len(input.Values) != len(args)-offset {
		return nil, fmt.Errorf("invalid persistence arguments")
	}
	for i, item := range input.Values {
		args[i+offset], err = e.decode(ctx, item, typ.In(i+offset))
		if err != nil {
			return nil, err
		}
	}
	output := fn.Call(args)
	resultValues := make([]value, len(output))
	var cleanup []string
	defer func() { e.release(cleanup) }()
	for i, item := range output {
		resultValues[i], err = e.encode(ctx, item, &cleanup)
		if err != nil {
			return nil, err
		}
	}
	return response{Values: resultValues}, nil
}

func (e *Endpoint) call(ctx context.Context, method, name string, args []reflect.Value, types []reflect.Type) ([]reflect.Value, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	input := request{Name: name, Values: make([]value, len(args))}
	var cleanup []string
	defer func() { e.release(cleanup) }()
	for i, arg := range args {
		var err error
		input.Values[i], err = e.encode(ctx, arg, &cleanup)
		if err != nil {
			return nil, err
		}
	}
	var output response
	if err := e.Peer.Call(ctx, method, input, &output); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, domain.Fail("TEMPORARY_FAILURE", "Persistence channel failed; verify the operation result before retrying")
	}
	if len(output.Values) != len(types) {
		return nil, fmt.Errorf("invalid persistence result")
	}
	result := make([]reflect.Value, len(types))
	for i, item := range output.Values {
		var err error
		result[i], err = e.decode(ctx, item, types[i])
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// Invoke is used by generated typed proxies, preserving internal exported fields
// independently of public API JSON tags. Calls are never automatically retried.
func (e *Endpoint) Invoke(ctx context.Context, name string, args, results []any) (invokeErr error) {
	e.mu.Lock()
	contract, exists := e.contracts[name]
	e.mu.Unlock()
	if !exists || contract.NumIn() == 0 || contract.In(0) != contextType || len(args) != contract.NumIn()-1 {
		return fmt.Errorf("invalid persistence invocation")
	}
	// 仅记录已经绑定的契约方法名，参数与返回值不进入日志。
	finish := pluginlog.Operation(pluginlog.Context(ctx, "musicfs.plugin.storage.sqlite", ""), name)
	defer func() { finish(invokeErr) }()
	input := make([]reflect.Value, len(args))
	for i, arg := range args {
		expected := contract.In(i + 1)
		input[i] = reflect.New(expected).Elem()
		if arg != nil {
			value := reflect.ValueOf(arg)
			if !value.Type().AssignableTo(expected) {
				return fmt.Errorf("invalid persistence argument type")
			}
			input[i].Set(value)
		} else {
			switch expected.Kind() {
			case reflect.Func, reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
			default:
				return fmt.Errorf("invalid nil persistence argument")
			}
		}
	}
	types := resultTypes(contract)
	hasError := len(types) > 0 && types[len(types)-1] == errorType
	expectedResults := len(types)
	if hasError {
		expectedResults--
	}
	if len(results) != expectedResults {
		return fmt.Errorf("invalid persistence result count")
	}
	for i, target := range results {
		value := reflect.ValueOf(target)
		if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() || value.Type().Elem() != types[i] {
			return fmt.Errorf("invalid persistence result target")
		}
	}
	output, err := e.call(ctx, "invoke", name, input, types)
	if err != nil {
		return err
	}
	// Return values on failed operations carry revision or recovery evidence in
	// some contracts and must remain identical to an in-process implementation.
	for i, target := range results {
		reflect.ValueOf(target).Elem().Set(output[i])
	}
	if hasError && !output[len(results)].IsNil() {
		return output[len(results)].Interface().(error)
	}
	return nil
}
