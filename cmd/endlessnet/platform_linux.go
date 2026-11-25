package main

import (
	"errors"
	"os"

	"github.com/endless-net/client/internal/headless"
	"golang.org/x/sys/unix"
)

var openPlatform = headless.OpenLinux

const platformHelp = "\nsudo endlessnet check-updates — проверить обновление через APT"

func platformCommand(args []string) (bool, error) {
	if len(args) != 1 || args[0] != "check-updates" {
		return false, nil
	}
	const helper = "/usr/libexec/endlessnet-client/check-updates"
	if unix.Exec(helper, []string{helper, args[0]}, os.Environ()) != nil {
		return true, errors.New("не найдена команда обновления установленного пакета Client")
	}
	return true, nil
}
