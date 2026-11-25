package main

import "github.com/endless-net/client/internal/headless"

var openPlatform = headless.OpenWindows

const platformHelp = ""

func platformCommand([]string) (bool, error) { return false, nil }
