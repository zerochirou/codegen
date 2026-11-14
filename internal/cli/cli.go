package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/zerochirou/codegen/internal/chroma"
	"github.com/zerochirou/codegen/internal/initwizard"
	"github.com/zerochirou/codegen/internal/mcp"
	"github.com/zerochirou/codegen/internal/tui"
)

// Version is overridden at build time via -ldflags "-X github.com/zerochirou/codegen/internal/cli.Version=...".
var Version = "0.2.0"

// Deps allows tests to stub side-effecting entrypoints cleanly.
type Deps struct {
	SkillsFS fs.FS
	RunTUI   func(opts tui.Options) error
	RunMCP   func(ctx context.Context) error
	RunInit  func(opts initwizard.Options) (initwizard.Result, error)
	Out      io.Writer
	Err      io.Writer
}

func defaultDeps(skillsFS fs.FS) Deps {
	return Deps{
		SkillsFS: skillsFS,
		RunTUI:   tui.Run,
		RunMCP: func(ctx context.Context) error {
			name := os.Getenv("MCP_SERVER_NAME")
			srv := mcp.NewServer(name, Version, chroma.NewManager())
			return srv.Serve(ctx)
		},
		RunInit: initwizard.Run,
		Out:     os.Stdout,
		Err:     os.Stderr,
	}
}

// NewRootCmd constructs the Cobra root command.
func NewRootCmd(deps Deps) *cobra.Command {
	if deps.Out == nil {
		deps.Out = os.Stdout
	}
	if deps.Err == nil {
		deps.Err = os.Stderr
	}

	root := &cobra.Command{
		Use:     "codegen",
		Short:   "codegen CLI — ChromaDB vector memory, MCP server, and MemoryTUI",
		Version: Version,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(deps.Out)
	root.SetErr(deps.Err)
	root.Flags().BoolP("version", "v", false, "Show codegen version")
	root.SetVersionTemplate("codegen {{.Version}}\n")

	// version subcommand
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Show codegen version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "codegen %s\n", Version)
		},
	})

	// tui subcommand
	var tuiGlobal bool
	var tuiCollection string
	tuiCmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the MemoryTUI (per-project by default, or --global)",
		RunE: func(_ *cobra.Command, _ []string) error {
			persistDir := chroma.ResolvePersistDir(tuiGlobal)
			return deps.RunTUI(tui.Options{
				PersistDir:     persistDir,
				CollectionName: tuiCollection,
				IsGlobal:       tuiGlobal,
			})
		},
	}
	tuiCmd.Flags().BoolVarP(&tuiGlobal, "global", "g", false, "Use the global codegen memory database (~/.codegen/chroma_data)")
	tuiCmd.Flags().StringVarP(&tuiCollection, "collection", "c", "", "ChromaDB collection name (defaults to CHROMA_COLLECTION_NAME or 'default')")
	root.AddCommand(tuiCmd)

	// run subcommand (MCP server)
	root.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Run the codegen-memory MCP server over stdio",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return deps.RunMCP(ctx)
		},
	})

	// init subcommand
	var initProject bool
	var initGlobal bool
	var initName string
	var initPath string
	var initYes bool
	var initNoSkills bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize codegen-memory and agent skills in the current project or globally",
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope := ""
			if initGlobal {
				scope = "global"
			} else if initProject {
				scope = "project"
			}
			var installSkills *bool
			if cmd.Flags().Changed("no-skills") {
				v := !initNoSkills
				installSkills = &v
			}
			_, err := deps.RunInit(initwizard.Options{
				Scope:         scope,
				ProjectName:   initName,
				StoragePath:   initPath,
				InstallSkills: installSkills,
				Yes:           initYes,
				SkillsFS:      deps.SkillsFS,
				In:            os.Stdin,
				Out:           cmd.OutOrStdout(),
			})
			return err
		},
	}
	initCmd.Flags().BoolVar(&initProject, "project", false, "Configure per-project scope")
	initCmd.Flags().BoolVar(&initGlobal, "global", false, "Configure global scope")
	initCmd.Flags().StringVar(&initName, "name", "", "Project name")
	initCmd.Flags().StringVar(&initPath, "path", "", "ChromaDB base or storage path")
	initCmd.Flags().BoolVarP(&initYes, "yes", "y", false, "Accept defaults without prompting")
	initCmd.Flags().BoolVar(&initNoSkills, "no-skills", false, "Skip installing agent skills")
	root.AddCommand(initCmd)

	// update subcommand
	root.AddCommand(&cobra.Command{
		Use:   "update",
		Short: "Update the global codegen CLI from GitHub using go install",
		RunE: func(cmd *cobra.Command, _ []string) error {
			goBin, err := exec.LookPath("go")
			if err != nil {
				return fmt.Errorf("`go` toolchain is required to update codegen")
			}
			c := exec.CommandContext(cmd.Context(), goBin, "install", "github.com/zerochirou/codegen/cmd/codegen@latest")
			c.Stdout = cmd.OutOrStdout()
			c.Stderr = cmd.ErrOrStderr()
			return c.Run()
		},
	})

	// uninstall subcommand
	root.AddCommand(&cobra.Command{
		Use:     "uninstall",
		Aliases: []string{"remove"},
		Short:   "Uninstall the global codegen CLI binary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, _ := os.UserHomeDir()
			candidates := []string{
				filepath.Join(home, ".local", "bin", "codegen"),
				filepath.Join(home, "go", "bin", "codegen"),
			}
			if exe, err := os.Executable(); err == nil {
				candidates = append(candidates, exe)
			}
			removed := 0
			seen := map[string]bool{}
			for _, p := range candidates {
				clean := filepath.Clean(p)
				if seen[clean] {
					continue
				}
				seen[clean] = true
				if _, err := os.Stat(clean); err == nil {
					if err := os.Remove(clean); err != nil {
						return fmt.Errorf("remove %s: %w", clean, err)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", clean)
					removed++
				}
			}
			if removed == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No installed codegen binary found.")
			}
			return nil
		},
	})

	return root
}

// Execute runs the CLI with bundled skills and returns the exit code.
func Execute(skillsFS fs.FS, args []string) int {
	cmd := NewRootCmd(defaultDeps(skillsFS))
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}
