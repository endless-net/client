//go:build windows || linux

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

// The CI runner checks the contract digest against the published SDK pin.
const contractSHA256 = "ea9131e97cc16e629a4e88009a7ed51fc0f20229b5320f9d6f8617f14793f738"

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "EndlessNet:", err)
		os.Exit(1)
	}
}

func run() error {
	if handled, err := platformCommand(os.Args[1:]); handled {
		return err
	}
	if len(os.Args) == 2 && os.Args[1] == "--print-contract" {
		_, err := fmt.Fprintln(os.Stdout, contractSHA256)
		return err
	}
	if len(os.Args) == 2 && (os.Args[1] == "--help" || os.Args[1] == "help") {
		_, err := fmt.Fprintln(os.Stdout, "endlessnet status — состояние службы и соединения\nendlessnet down — отключить туннель, сохранив регистрацию\nendlessnet up — войти в EndlessNet и подключиться\nendlessnet enroll ACCOUNT NETWORK — подать заявку на регистрацию сервера; ключ читается из файла или канала через stdin"+platformHelp)
		return err
	}
	enroll := len(os.Args) == 4 && os.Args[1] == "enroll"
	if !enroll && (len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "status" && os.Args[1] != "down")) {
		return fmt.Errorf("использование команды описано в endlessnet --help")
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
	channel, err := openPlatform(ctx, digest)
	if err != nil {
		return err
	}
	defer func() { _ = channel.Close() }()
	client := clientv0.NewClientServiceClient(channel)
	if enroll {
		info, err := os.Stdin.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice != 0 {
			return fmt.Errorf("передайте ключ через перенаправленный стандартный ввод")
		}
		stopRead := context.AfterFunc(ctx, func() { _ = os.Stdin.Close() })
		defer stopRead()
		return headless.EnrollWithKey(ctx, client, os.Args[2], os.Args[3], os.Stdin, os.Stdout)
	}
	return headless.Run(ctx, client, os.Args[1], os.Stdout)
}
