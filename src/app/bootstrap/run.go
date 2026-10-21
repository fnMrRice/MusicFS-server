package bootstrap

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

const APIVersion = "1"

func Run(args []string, output io.Writer) error {
	if len(args) > 0 && (args[0] == "backup" || args[0] == "verify-backup" || args[0] == "restore") {
		return runBackup(args[0], args[1:], output)
	}
	if len(args) == 1 && args[0] == "token" {
		_, err := io.WriteString(output, domain.NewID()+"\n")
		return err
	}
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	f := flag.NewFlagSet("musicfs", flag.ContinueOnError)
	f.SetOutput(output)
	path := f.String("config", "", "Configuration YAML path (.yaml or .yml)")
	web := f.String("web-root", "", "Built Web artifact directory")
	startupMode := f.String("startup-mode", "standalone", "Startup ownership: standalone or client-managed")
	version := f.Bool("version", false, "Print version")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *version {
		_, err := io.WriteString(output, "MusicFS Server 0.1.0 (API v1)\n")
		return err
	}
	if *startupMode != "standalone" && *startupMode != "client-managed" {
		return domain.Fail("INVALID_ARGUMENT", "Startup mode must be standalone or client-managed")
	}
	if f.NArg() != 0 {
		return domain.Fail("INVALID_ARGUMENT", "Unexpected command arguments")
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return ServeWithOptions(ctx, c, slog.New(slog.NewJSONHandler(output, nil)), ServeOptions{StartupMode: *startupMode})
}
