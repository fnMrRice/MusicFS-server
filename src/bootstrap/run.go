package bootstrap

import (
	"context"
	"flag"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"io"
	"log/slog"
	"os"
	"os/signal"
)

const APIVersion = "1"

func Run(args []string, output io.Writer) error {
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	f := flag.NewFlagSet("musicfs", flag.ContinueOnError)
	f.SetOutput(output)
	path := f.String("config", "", "Configuration JSON path")
	web := f.String("web-root", "", "Built Web artifact directory")
	version := f.Bool("version", false, "Print version")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *version {
		_, err := io.WriteString(output, "MusicFS Server 0.1.0 (API v1)\n")
		return err
	}
	overrides := map[string]string{}
	f.Visit(func(v *flag.Flag) {
		if v.Name == "web-root" {
			overrides["WEB_ROOT"] = *web
		}
	})
	c, err := config.Load(*path, overrides)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return Serve(ctx, c, slog.New(slog.NewJSONHandler(output, nil)))
}
