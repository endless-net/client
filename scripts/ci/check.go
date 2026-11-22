package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func goEnvironment(extra ...string) []string {
	return append([]string{"GOWORK=off", "GOFLAGS=-mod=readonly"}, extra...)
}

func (r runner) check(ctx context.Context, profile profile, lintConfig string, goOnly bool) error {
	if lintConfig != "" {
		path, err := filepath.Abs(lintConfig)
		if err != nil {
			return err
		}
		lintConfig = path
	}
	for _, module := range profile.Modules {
		if goOnly && module.Path == "api" {
			continue
		}
		where := filepath.Join(r.root, module.Path)
		fmt.Println("CHECK", module.Kind, module.Path)
		if module.Kind == "dart" {
			commands := [][]string{{"dart", "pub", "get", "--enforce-lockfile"},
				{"dart", "format", "--output=none", "--set-exit-if-changed", "."}, {"dart", "analyze"}}
			if len(module.Unit) > 0 {
				commands = append(commands, append([]string{"dart", "test"}, module.Unit...))
			}
			for _, args := range commands {
				output, err := r.run(ctx, where, nil, args...)
				if err != nil {
					return err
				}
				fmt.Print(string(output))
			}
			if len(module.Unit) == 0 {
				fmt.Println("UNIT CASES ABSENT: tooling; no coverage claim")
			}
			continue
		}
		config := lintConfig
		if config == "" {
			config = filepath.Join(where, ".golangci-lint.yaml")
		}
		if err := r.checkGo(ctx, where, config, goOnly); err != nil {
			return err
		}
	}
	return nil
}

func (r runner) checkGo(ctx context.Context, where, config string, goOnly bool) error {
	env := goEnvironment()
	metadata, err := r.metadata(ctx, where)
	if err != nil {
		return err
	}
	for _, entry := range metadata.Replace {
		if strings.HasPrefix(entry.Old.Path, "github.com/endless-net/") {
			return errors.New("first-party replace is forbidden")
		}
	}
	if where == r.root {
		if err := r.verifyClientContract(ctx); err != nil {
			return err
		}
	}
	pins, err := r.verifyCrypto(ctx, where, metadata, true)
	if err != nil {
		return err
	}
	if !goOnly {
		output, err := r.run(ctx, where, env, "goimports", "-l", ".")
		if err != nil {
			return err
		}
		if len(output) != 0 {
			return fmt.Errorf("go source formatting differs:\n%s", output)
		}
		if _, err := r.run(ctx, where, env, "go", "vet", "./..."); err != nil {
			return err
		}
	}
	for _, args := range [][]string{
		{"golangci-lint", "run", "--config", config, "./...", "--timeout", "1m"},
		{"go", "test", "-short", "./..."},
		{"govulncheck", "./..."},
	} {
		output, err := r.run(ctx, where, env, args...)
		if err != nil {
			return err
		}
		fmt.Print(string(output))
	}
	if len(pins) > 0 {
		return r.auditCrypto(ctx, where, pins)
	}
	return nil
}
