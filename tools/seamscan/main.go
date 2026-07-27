// Command seamscan reports candidate test-quality problems: assertions that
// cannot fail, and handoffs between components that no test exercises.
//
// It emits candidates only. Deciding whether a candidate is a real defect is
// the job of the audit-test-seams skill.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "seamscan:", err)
		os.Exit(1)
	}
}

// run takes an io.Writer rather than *os.File so a test can capture output.
func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: seamscan weak-assertions [--text] <path> [<path> ...]")
	}

	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	text := fs.Bool("text", false, "human-readable output instead of JSON")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	paths := fs.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	var (
		found []Finding
		err   error
	)
	switch cmd {
	case "weak-assertions":
		found, err = WeakAssertions(paths)
	default:
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
	if err != nil {
		return err
	}

	if *text {
		EmitText(stdout, found)
		return nil
	}
	return EmitJSON(stdout, found)
}
