// Command vaquero is the Vaquero ticket tracker: its server and its CLI.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/stazelabs/vaquero/internal/version"
)

const usage = `usage: vaquero <command>

commands:
  serve      run the server
  version    print the release, commit and build date
  help       show this message
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is main, testable: it returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version.Line())
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
	return 0
}
