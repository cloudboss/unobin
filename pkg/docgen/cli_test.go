package docgen

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIReference(t *testing.T) {
	tests := []struct {
		name string
		root func() *cobra.Command
		want string
	}{
		{
			name: "intro only, no subcommands",
			root: func() *cobra.Command {
				return &cobra.Command{Use: "tool", Short: "Does things."}
			},
			want: "# CLI reference\n\nDoes things.\n",
		},
		{
			name: "command with a flag",
			root: func() *cobra.Command {
				root := &cobra.Command{Use: "tool", Short: "Does things."}
				greet := &cobra.Command{Use: "greet", Short: "Say hello.", Run: noop}
				greet.Flags().String("name", "world", "Who to greet.")
				root.AddCommand(greet)
				return root
			},
			want: "# CLI reference\n\nDoes things.\n" +
				"\n## tool greet\n\nSay hello.\n" +
				"\n```\ntool greet [flags]\n```\n" +
				"\n**Flags**\n\n" +
				"| Flag | Default | Description |\n" +
				"| --- | --- | --- |\n" +
				"| `--name string` | `world` | Who to greet. |\n",
		},
		{
			name: "shorthand, bool, and empty default render correctly",
			root: func() *cobra.Command {
				root := &cobra.Command{Use: "tool"}
				c := &cobra.Command{Use: "build", Short: "Build it.", Run: noop}
				c.Flags().StringP("out", "o", "", "Output path.")
				c.Flags().Bool("force", false, "Overwrite existing files.")
				root.AddCommand(c)
				return root
			},
			want: "# CLI reference\n" +
				"\n## tool build\n\nBuild it.\n" +
				"\n```\ntool build [flags]\n```\n" +
				"\n**Flags**\n\n" +
				"| Flag | Default | Description |\n" +
				"| --- | --- | --- |\n" +
				"| `--force` | `false` | Overwrite existing files. |\n" +
				"| `-o, --out string` |  | Output path. |\n",
		},
		{
			name: "long description preferred over short",
			root: func() *cobra.Command {
				root := &cobra.Command{Use: "tool"}
				c := &cobra.Command{
					Use:   "go",
					Short: "short text",
					Long:  "The long description.",
					Run:   noop,
				}
				root.AddCommand(c)
				return root
			},
			want: "# CLI reference\n" +
				"\n## tool go\n\nThe long description.\n" +
				"\n```\ntool go\n```\n",
		},
		{
			name: "parent command with nested subcommand",
			root: func() *cobra.Command {
				root := &cobra.Command{Use: "tool"}
				gen := &cobra.Command{Use: "generate", Short: "Generate things."}
				stack := &cobra.Command{Use: "stack", Short: "Scaffold a stack.", Run: noop}
				gen.AddCommand(stack)
				root.AddCommand(gen)
				return root
			},
			want: "# CLI reference\n" +
				"\n## tool generate\n\nGenerate things.\n" +
				"\n### tool generate stack\n\nScaffold a stack.\n" +
				"\n```\ntool generate stack\n```\n",
		},
		{
			name: "hidden flag is omitted",
			root: func() *cobra.Command {
				root := &cobra.Command{Use: "tool"}
				c := &cobra.Command{Use: "run", Short: "Run it.", Run: noop}
				c.Flags().String("shown", "", "Visible flag.")
				c.Flags().String("secret", "", "Hidden flag.")
				_ = c.Flags().MarkHidden("secret")
				root.AddCommand(c)
				return root
			},
			want: "# CLI reference\n" +
				"\n## tool run\n\nRun it.\n" +
				"\n```\ntool run [flags]\n```\n" +
				"\n**Flags**\n\n" +
				"| Flag | Default | Description |\n" +
				"| --- | --- | --- |\n" +
				"| `--shown string` |  | Visible flag. |\n",
		},
		{
			name: "pipe in usage is escaped",
			root: func() *cobra.Command {
				root := &cobra.Command{Use: "tool"}
				c := &cobra.Command{Use: "out", Short: "Write.", Run: noop}
				c.Flags().String("dest", "", "A file, or `-` for stdout.")
				c.Flags().String("mode", "", "One of a|b|c.")
				root.AddCommand(c)
				return root
			},
			want: "# CLI reference\n" +
				"\n## tool out\n\nWrite.\n" +
				"\n```\ntool out [flags]\n```\n" +
				"\n**Flags**\n\n" +
				"| Flag | Default | Description |\n" +
				"| --- | --- | --- |\n" +
				"| `--dest string` |  | A file, or `-` for stdout. |\n" +
				"| `--mode string` |  | One of a\\|b\\|c. |\n",
		},
		{
			name: "auto help flag is omitted from the table",
			root: func() *cobra.Command {
				root := &cobra.Command{Use: "tool"}
				c := &cobra.Command{Use: "go", Short: "Go.", Run: noop}
				c.InitDefaultHelpFlag()
				root.AddCommand(c)
				return root
			},
			want: "# CLI reference\n" +
				"\n## tool go\n\nGo.\n" +
				"\n```\ntool go [flags]\n```\n",
		},
		{
			name: "help and completion subcommands are skipped",
			root: func() *cobra.Command {
				root := &cobra.Command{Use: "tool"}
				root.InitDefaultHelpCmd()
				root.InitDefaultCompletionCmd()
				real := &cobra.Command{Use: "real", Short: "A real command.", Run: noop}
				root.AddCommand(real)
				return root
			},
			want: "# CLI reference\n" +
				"\n## tool real\n\nA real command.\n" +
				"\n```\ntool real\n```\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CLIReference(tt.root())
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCLIReferenceIsDeterministic(t *testing.T) {
	build := func() *cobra.Command {
		root := &cobra.Command{Use: "tool", Short: "Does things."}
		for _, name := range []string{"zeta", "alpha", "mike"} {
			c := &cobra.Command{Use: name, Short: "A " + name + " command.", Run: noop}
			c.Flags().String("two", "", "Second flag.")
			c.Flags().String("one", "", "First flag.")
			root.AddCommand(c)
		}
		return root
	}
	first := CLIReference(build())
	for range 5 {
		require.Equal(t, first, CLIReference(build()))
	}
	// Commands and flags must come out lexically ordered regardless of
	// the order they were added.
	assert.Less(t, indexOf(first, "tool alpha"), indexOf(first, "tool mike"))
	assert.Less(t, indexOf(first, "tool mike"), indexOf(first, "tool zeta"))
	assert.Less(t, indexOf(first, "--one"), indexOf(first, "--two"))
}

func noop(_ *cobra.Command, _ []string) {}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
