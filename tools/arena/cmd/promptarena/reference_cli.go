package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const helpCmdName = "help"

// generateCLIReference renders the subcommand catalog by walking rootCmd. It
// lists each user-facing command, its short help, and key flags.
func generateCLIReference() []byte {
	var b bytes.Buffer
	b.WriteString("# CLI Reference\n\n")
	b.WriteString("Generated from the command tree. Run `promptarena <cmd> --help` for full detail.\n\n")

	cmds := append([]*cobra.Command{}, rootCmd.Commands()...)
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })

	for _, c := range cmds {
		if c.Hidden || c.Name() == helpCmdName || c.Name() == "completion" {
			continue
		}
		fmt.Fprintf(&b, "## promptarena %s\n\n%s\n\n", c.Name(), c.Short)
		writeFlagList(&b, c)
		for _, sub := range sortedSubs(c) {
			fmt.Fprintf(&b, "- `promptarena %s %s` — %s\n", c.Name(), sub.Name(), sub.Short)
		}
		if len(c.Commands()) > 0 {
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}

func sortedSubs(c *cobra.Command) []*cobra.Command {
	subs := append([]*cobra.Command{}, c.Commands()...)
	sort.Slice(subs, func(i, j int) bool { return subs[i].Name() < subs[j].Name() })
	out := subs[:0]
	for _, s := range subs {
		if !s.Hidden && s.Name() != helpCmdName {
			out = append(out, s)
		}
	}
	return out
}

func writeFlagList(b *bytes.Buffer, c *cobra.Command) {
	var flags []string
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		flags = append(flags, "`--"+f.Name+"`")
	})
	if len(flags) == 0 {
		return
	}
	sort.Strings(flags)
	fmt.Fprintf(b, "Flags: %s\n\n", strings.Join(flags, ", "))
}
