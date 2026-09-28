// Command contract-shards selects one complete scenario shard from a compiled inventory.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/endless-net/client/internal/contractshard"
)

func main() {
	if len(os.Args) != 5 {
		fail("usage: contract-shards INVENTORY SHARD(1|2) SELECTED_FILE PATTERN_FILE")
	}
	index, err := strconv.Atoi(os.Args[2])
	if err != nil || index < 1 || index > contractshard.Count {
		fail("invalid shard index")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail("cannot read compiled inventory")
	}
	names, err := contractshard.ParseInventory(data)
	if err != nil {
		fail(err.Error())
	}
	if err := contractshard.RequireFlowRoots(names); err != nil {
		fail(err.Error())
	}
	selected := contractshard.Split(names)[index-1]
	if len(selected) == 0 {
		fail("empty scenario shard")
	}
	if err := os.WriteFile(os.Args[3], []byte(strings.Join(selected, "\n")+"\n"), 0o600); err != nil {
		fail("cannot write selected inventory")
	}
	if err := os.WriteFile(os.Args[4], []byte(contractshard.Pattern(selected)+"\n"), 0o600); err != nil {
		fail("cannot write test selection pattern")
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
