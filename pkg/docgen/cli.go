// Package docgen renders unobin reference documentation as Markdown for
// the docs site.
package docgen

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CLIReference renders the command tree rooted at root as a single
// Markdown page: an intro from the root's own description, then one
// section per visible command, nested by depth. Commands and flags are
// emitted in lexical order, so the output is identical on every run and
// can be regenerated without churn.
func CLIReference(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString("# CLI reference\n")
	if d := describe(root); d != "" {
		fmt.Fprintf(&b, "\n%s\n", d)
	}
	for _, c := range visibleSubcommands(root) {
		writeCommand(&b, c, 2)
	}
	return b.String()
}

func writeCommand(b *strings.Builder, cmd *cobra.Command, level int) {
	fmt.Fprintf(b, "\n%s %s\n", strings.Repeat("#", level), cmd.CommandPath())
	if d := describe(cmd); d != "" {
		fmt.Fprintf(b, "\n%s\n", d)
	}
	if cmd.Runnable() {
		fmt.Fprintf(b, "\n```\n%s\n```\n", cmd.UseLine())
	}
	if t := flagsTable(cmd); t != "" {
		fmt.Fprintf(b, "\n**Flags**\n\n%s", t)
	}
	for _, c := range visibleSubcommands(cmd) {
		writeCommand(b, c, level+1)
	}
}

func describe(cmd *cobra.Command) string {
	if cmd.Long != "" {
		return strings.TrimSpace(cmd.Long)
	}
	return strings.TrimSpace(cmd.Short)
}

// visibleSubcommands returns cmd's child commands worth documenting, in
// lexical order: no hidden, deprecated, or help-topic commands, and not
// cobra's auto-added help and completion commands.
func visibleSubcommands(cmd *cobra.Command) []*cobra.Command {
	var cmds []*cobra.Command
	for _, c := range cmd.Commands() {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		cmds = append(cmds, c)
	}
	slices.SortFunc(cmds, func(a, b *cobra.Command) int {
		return cmp.Compare(a.Name(), b.Name())
	})
	return cmds
}

// flagsTable renders cmd's own (non-inherited) flags as a Markdown table,
// or "" when there are none. pflag visits flags in lexical order.
func flagsTable(cmd *cobra.Command) string {
	var b strings.Builder
	cmd.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		if b.Len() == 0 {
			b.WriteString("| Flag | Default | Description |\n")
			b.WriteString("| --- | --- | --- |\n")
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n",
			flagName(f), flagDefault(f), cellEscape(f.Usage))
	})
	return b.String()
}

func flagName(f *pflag.Flag) string {
	var name strings.Builder
	if f.Shorthand != "" {
		fmt.Fprintf(&name, "-%s, ", f.Shorthand)
	}
	fmt.Fprintf(&name, "--%s", f.Name)
	if t := f.Value.Type(); t != "bool" {
		fmt.Fprintf(&name, " %s", t)
	}
	return "`" + name.String() + "`"
}

func flagDefault(f *pflag.Flag) string {
	if f.DefValue == "" {
		return ""
	}
	return "`" + f.DefValue + "`"
}

// cellEscape flattens a flag's usage text onto one line and escapes the
// pipe so it does not break the Markdown table.
func cellEscape(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.TrimSpace(s)
}
