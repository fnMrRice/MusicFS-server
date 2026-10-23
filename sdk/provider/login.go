package provider

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

func (w *worker) loginOptions(ctx context.Context, client Client) (online.AccountOptions, error) {
	d := w.plugin.Descriptor.Login
	if d == nil {
		return online.AccountOptions{Method: "none", Validation: "notSupported"}, nil
	}
	options := online.AccountOptions{Method: d.Method, Configured: true, Validation: d.Validation}
	if provider, ok := client.(interface {
		AccountOptions(context.Context) (online.AccountOptions, error)
	}); ok {
		var err error
		options, err = provider.AccountOptions(ctx)
		if err != nil {
			return online.AccountOptions{}, err
		}
	}
	if err := options.Validate(d); err != nil {
		return online.AccountOptions{}, err
	}
	return options, nil
}
