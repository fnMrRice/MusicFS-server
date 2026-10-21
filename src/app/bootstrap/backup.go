package bootstrap

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/backup"
)

func runBackup(command string, args []string, output io.Writer) error {
	f := flag.NewFlagSet("musicfs "+command, flag.ContinueOnError)
	f.SetOutput(output)
	var configPath, parent, input, destination *string
	switch command {
	case "backup":
		configPath = f.String("config", "", "Configuration YAML path")
		parent = f.String("output", "", "Existing backup parent directory")
	case "verify-backup":
		input = f.String("input", "", "Completed backup directory")
	case "restore":
		input = f.String("input", "", "Completed backup directory")
		destination = f.String("destination", "", "New installation directory (must not exist)")
	}
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return domain.Fail("INVALID_ARGUMENT", "Unexpected maintenance arguments")
	}
	ctx := context.Background()
	abs := func(value string) (string, error) {
		if value == "" {
			return "", domain.Fail("INVALID_ARGUMENT", "Required maintenance path is missing")
		}
		return filepath.Abs(value)
	}
	var value any
	var err error
	switch command {
	case "backup":
		var doc *config.Document
		var path string
		path, err = abs(*parent)
		if err != nil {
			return err
		}
		// Offline filesystem access needs no live administrator credential.
		doc, err = config.ReadDocument(*configPath, map[string]string{"ADMIN_TOKEN": domain.NewID()})
		if err != nil {
			return err
		}
		value, err = backup.Create(ctx, doc, path)
	case "verify-backup":
		var path string
		path, err = abs(*input)
		if err != nil {
			return err
		}
		value, err = backup.Verify(ctx, path)
	case "restore":
		var path, target string
		path, err = abs(*input)
		if err != nil {
			return err
		}
		target, err = abs(*destination)
		if err != nil {
			return err
		}
		value, err = backup.Restore(ctx, path, target)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(value)
}
