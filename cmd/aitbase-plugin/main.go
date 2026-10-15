// Command aitbase-plugin creates, checks, builds and packages AITBASE VISION plugins.
package main

import (
	"os"

	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
