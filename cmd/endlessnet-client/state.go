package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/unng-lab/endlessnet-client/internal/client"
)

func cmdState(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("state command requires migrate")
	}
	switch args[0] {
	case "migrate":
		fs := flag.NewFlagSet("state migrate", flag.ExitOnError)
		configPath := fs.String("config", "", "client state path")
		backupPath := fs.String("backup", "", "protected backup path; defaults next to the client state")
		jsonOutput := fs.Bool("json", false, "write migration result as JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := client.MigrateConfigState(*configPath, *backupPath)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(result)
		}
		if !result.Migrated {
			fmt.Printf("client state is already %s version %d: %s\n", result.TargetFormat, result.TargetVersion, result.StatePath)
			return nil
		}
		fmt.Printf(
			"migrated client state from %s version %d to %s version %d: %s\nbackup: %s\n",
			result.SourceFormat,
			result.SourceVersion,
			result.TargetFormat,
			result.TargetVersion,
			result.StatePath,
			result.BackupPath,
		)
		return nil
	default:
		return fmt.Errorf("state command requires migrate")
	}
}
