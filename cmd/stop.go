package cmd

import (
	"fmt"

	"github.com/rossturk/rowner/internal/config"
	"github.com/rossturk/rowner/internal/githubapi"
	"github.com/rossturk/rowner/internal/incus"
	"github.com/rossturk/rowner/internal/state"
	"github.com/spf13/cobra"
)

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "stop <name>",
		Short:             "Stop the VM and unregister the runner from GitHub",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeRunnerNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			return doStopOrDestroy(args[0], false)
		},
	}
}

func destroyCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "destroy <name>",
		Short:             "Delete the VM and unregister the runner from GitHub",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeRunnerNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			return doStopOrDestroy(args[0], true)
		},
	}
}

// completeRunnerNames returns rowner-tracked runner names for shell completion.
// Used by `stop` and `destroy` so `rowner destroy <Tab>` shows live names.
// Only suggests when no positional arg has been given yet.
func completeRunnerNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	runners, err := state.All()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	out := make([]string, 0, len(runners))
	for _, r := range runners {
		// Each completion entry can optionally include a description after a tab.
		// Some shells (zsh, fish) display it; bash ignores it.
		out = append(out, r.Name+"\t"+r.Kind+" runner ("+r.Repo+")")
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func doStopOrDestroy(name string, destroy bool) error {
	s, err := state.Load(name)
	if err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("no rowner state for %q", name)
	}
	cfg, err := config.Load(".env")
	if err != nil {
		return err
	}
	gh := githubapi.New(cfg.PAT)

	r, err := gh.FindRunner(s.Repo, name)
	if err != nil {
		return err
	}
	if r == nil {
		fmt.Printf("==> runner %s not found on GitHub (already removed)\n", name)
	} else {
		fmt.Printf("==> deleting runner %s (id=%d) from GitHub\n", name, r.ID)
		if err := gh.DeleteRunner(s.Repo, r.ID); err != nil {
			return err
		}
	}

	if destroy {
		fmt.Printf("==> destroying VM %s\n", name)
		_ = incus.Delete(name)
		return state.Remove(name)
	}
	fmt.Printf("==> stopping VM %s\n", name)
	_ = incus.Stop(name)
	return nil
}
