package bootstrap

import "io"

const APIVersion = "1"

// Run is the composition boundary; infrastructure is added by subsequent steps.
func Run(args []string, output io.Writer) error {
	_, err := io.WriteString(output, "MusicFS Server: implementation in progress\n")
	return err
}
