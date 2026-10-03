// Command capacity-test runs the one-click capacity measurement in
// internal/capacity: plan validation, multi-agent load, verification, search
// and reports. See docs/DEVELOPMENT.md#容量验证.
package main

import (
	"fmt"
	"os"
)

func main() {
	if !subcommand(os.Args[1:]) {
		fmt.Fprint(os.Stderr, subcommandUsage)
		os.Exit(2)
	}
}
