// orbit is the entrypoint only — all behavior lives in internal/cmd.
package main

import (
	"github.com/orbit-sh/orbit-cli/internal/cmd"
)

func main() {
	cmd.Run()
}
