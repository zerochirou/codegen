package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zerochirou/codegen/internal/skills"
)

type skillSourceFlags struct {
	global      bool
	projectPath string
	remote      bool
	ref         string
}

func bindTargetAndSourceFlags(cmd *cobra.Command, f *skillSourceFlags) {
	cmd.Flags().BoolVarP(&f.global, "global", "g", false, "Use global skills (~/.gemini/config/skills) instead of current project")
	cmd.Flags().StringVarP(&f.projectPath, "path", "p", "", "Target project root directory (auto-detected if omitted)")
	cmd.Flags().BoolVar(&f.remote, "remote", false, "Fetch latest skills and rules from GitHub instead of the binary's bundled assets")
	cmd.Flags().StringVar(&f.ref, "ref", skills.DefaultRef, "Git branch, tag, or commit to fetch with --remote")
}

func resolveSourceFS(ctx context.Context, deps Deps, f skillSourceFlags) (fs.FS, string, error) {
	if f.remote {
		ref := strings.TrimSpace(f.ref)
		if ref == "" {
			ref = skills.DefaultRef
		}
		remoteFS, err := skills.FetchRemoteFS(ctx, skills.RemoteOptions{
			Ref:        ref,
			HTTPClient: deps.RemoteHTTPClient,
		})
		if err != nil {
			return nil, "", err
		}
		return remoteFS, fmt.Sprintf("remote (github.com/%s@%s)", skills.DefaultRepo, ref), nil
	}
	if deps.SkillsFS == nil {
		return nil, "", errors.New("bundled skills are unavailable in this binary; pass --remote to fetch from GitHub")
	}
	return deps.SkillsFS, fmt.Sprintf("bundled (codegen %s)", Version), nil
}

func newSkillsCmd(deps Deps) *cobra.Command {
	skillsCmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage and update bundled agent skills per project or globally",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	skillsCmd.AddCommand(newSkillsUpdateCmd(deps))
	skillsCmd.AddCommand(newSkillsListCmd(deps))
	return skillsCmd
}

func newSkillsUpdateCmd(deps Deps) *cobra.Command {
	var f skillSourceFlags
	var dryRun bool
	var noRules bool

	cmd := &cobra.Command{
		Use:     "update [skill...]",
		Aliases: []string{"sync"},
		Short:   "Install new or updated bundled agent skills in the current project (or --global)",
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := skills.ResolveTarget(skills.TargetOptions{
				Global:      f.global,
				ProjectPath: f.projectPath,
			})
			if err != nil {
				return err
			}

			srcFS, sourceLabel, err := resolveSourceFS(cmd.Context(), deps, f)
			if err != nil {
				return err
			}

			res, err := skills.Sync(skills.SyncOptions{
				Target:       target,
				SkillsFS:     srcFS,
				FilterNames:  args,
				IncludeRules: !noRules,
				DryRun:       dryRun,
			})
			if err != nil {
				return err
			}

			renderSyncResult(cmd.OutOrStdout(), res, sourceLabel, len(args) > 0)
			return nil
		},
	}

	bindTargetAndSourceFlags(cmd, &f)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview skill and rule updates without writing files")
	cmd.Flags().BoolVar(&noRules, "no-rules", false, "Skip updating always-on rules (.agents/rules)")
	return cmd
}

func newSkillsListCmd(deps Deps) *cobra.Command {
	var f skillSourceFlags
	var jsonOut bool

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "status"},
		Short:   "List bundled, installed, and custom skills with their update status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target, err := skills.ResolveTarget(skills.TargetOptions{
				Global:      f.global,
				ProjectPath: f.projectPath,
			})
			if err != nil {
				return err
			}

			srcFS, sourceLabel, err := resolveSourceFS(cmd.Context(), deps, f)
			if err != nil {
				return err
			}

			res, err := skills.Inspect(skills.SyncOptions{
				Target:       target,
				SkillsFS:     srcFS,
				IncludeRules: true,
			})
			if err != nil {
				return err
			}

			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			}

			renderListResult(cmd.OutOrStdout(), res, sourceLabel, f)
			return nil
		},
	}

	bindTargetAndSourceFlags(cmd, &f)
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output status as JSON")
	return cmd
}

func renderSyncResult(out io.Writer, res skills.SyncResult, sourceLabel string, filtered bool) {
	fmt.Fprintf(out, "Skills target: %s (%s)\n", res.SkillsDir, res.Scope)
	fmt.Fprintf(out, "Source: %s\n", sourceLabel)
	if res.DryRun {
		fmt.Fprintln(out, "Mode: dry-run (no files modified)")
	}
	fmt.Fprintln(out)

	unchangedCount := 0
	for _, item := range res.Skills {
		switch item.Status {
		case skills.StatusAdded, skills.StatusNew:
			fmt.Fprintf(out, "  + %s (%s)\n", item.Name, item.Status)
		case skills.StatusUpdated, skills.StatusOutdated:
			fmt.Fprintf(out, "  ~ %s (%s)\n", item.Name, item.Status)
		case skills.StatusUpToDate:
			if filtered {
				fmt.Fprintf(out, "  = %s (%s)\n", item.Name, item.Status)
			} else {
				unchangedCount++
			}
		}
	}
	if !filtered && unchangedCount > 0 {
		fmt.Fprintf(out, "  = %d bundled skill(s) already up-to-date\n", unchangedCount)
	}
	if len(res.CustomSkills) > 0 {
		fmt.Fprintf(out, "  * %d custom skill(s) preserved (%s)\n", len(res.CustomSkills), strings.Join(res.CustomSkills, ", "))
	}

	if len(res.Rules) > 0 {
		fmt.Fprintf(out, "\nRules target: %s\n", res.RulesDir)
		for _, r := range res.Rules {
			marker := "="
			switch r.Status {
			case skills.StatusAdded, skills.StatusNew:
				marker = "+"
			case skills.StatusUpdated, skills.StatusOutdated:
				marker = "~"
			}
			fmt.Fprintf(out, "  %s %s (%s)\n", marker, r.Name, r.Status)
		}
	}

	fmt.Fprintln(out)
	if res.DryRun {
		fmt.Fprintf(out, "Dry run: %d to add, %d to update, %d up-to-date.\n", res.AddedCount, res.UpdatedCount, res.UnchangedCount)
		return
	}
	fmt.Fprintf(out, "Done: %d added, %d updated, %d up-to-date.\n", res.AddedCount, res.UpdatedCount, res.UnchangedCount)
}

func renderListResult(out io.Writer, res skills.SyncResult, sourceLabel string, f skillSourceFlags) {
	fmt.Fprintf(out, "Skills target: %s (%s)\n", res.SkillsDir, res.Scope)
	fmt.Fprintf(out, "Source: %s\n\n", sourceLabel)

	fmt.Fprintf(out, "Bundled skills (%d):\n", len(res.Skills))
	for _, item := range res.Skills {
		fmt.Fprintf(out, "  %-12s %s\n", "["+string(item.Status)+"]", item.Name)
	}

	if len(res.CustomSkills) > 0 {
		fmt.Fprintf(out, "\nCustom skills (%d):\n", len(res.CustomSkills))
		for _, name := range res.CustomSkills {
			fmt.Fprintf(out, "  %-12s %s\n", "["+string(skills.StatusCustom)+"]", name)
		}
	}

	if len(res.Rules) > 0 {
		fmt.Fprintf(out, "\nRules (%s):\n", res.RulesDir)
		for _, r := range res.Rules {
			fmt.Fprintf(out, "  %-12s %s\n", "["+string(r.Status)+"]", r.Name)
		}
	}

	ruleChanges := 0
	for _, r := range res.Rules {
		if r.Status != skills.StatusUpToDate {
			ruleChanges++
		}
	}
	pending := res.AddedCount + res.UpdatedCount + ruleChanges
	fmt.Fprintln(out)
	if pending == 0 {
		fmt.Fprintln(out, "All bundled skills and rules are up-to-date.")
		return
	}

	hint := "codegen skills update"
	if f.global {
		hint += " --global"
	}
	if f.remote {
		hint += " --remote"
	}
	fmt.Fprintf(out, "Run '%s' to apply %d pending update(s).\n", hint, pending)
}
