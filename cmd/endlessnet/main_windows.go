//go:build windows

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/headless"
)

// Exact published Client API0.5.0 descriptor; no compatibility negotiation.
const contractSHA256 = "b57f96e0768571364ef88dc08f5b06375a52ac38eceabee9267a902ceb526817"

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "EndlessNet:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "--print-contract" {
		_, err := fmt.Fprintln(os.Stdout, contractSHA256)
		return err
	}
	if len(os.Args) == 2 && (os.Args[1] == "--help" || os.Args[1] == "help") {
		_, err := fmt.Fprintln(os.Stdout, "endlessnet status — состояние службы и соединения\nendlessnet down — отключить туннель, сохранив регистрацию\nendlessnet up — войти в EndlessNet и подключиться")
		return err
	}
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "status" && os.Args[1] != "down") {
		return fmt.Errorf("использование: endlessnet up|status|down; подробности: endlessnet --help")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	budget := 30 * time.Second
	if os.Args[1] == "up" {
		budget = 22 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	raw, err := hex.DecodeString(contractSHA256)
	if err != nil || len(raw) != 32 {
		return fmt.Errorf("неверная версия контракта сборки")
	}
	var digest [32]byte
	copy(digest[:], raw)
	channel, err := headless.OpenWindows(ctx, digest)
	if err != nil {
		return err
	}
	defer func() { _ = channel.Close() }()
	return headless.Run(ctx, clientv0.NewClientServiceClient(channel), os.Args[1], os.Stdout)
}
