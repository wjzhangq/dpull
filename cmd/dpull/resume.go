package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wjzhangq/dpull/internal/store"
)

var resumeCmd = &cobra.Command{
	Use:   "resume [TASK_ID]",
	Short: "Resume an interrupted download by task ID or canonical name",
	Long: `Resume an interrupted download.

If TASK_ID is provided, resumes that specific task.
If an image name is provided, searches for a matching task.
If no argument is given, lists available tasks.`,
	RunE: runResume,
}

func runResume(cmd *cobra.Command, args []string) error {
	// Initialize store
	st, err := store.NewStore(cacheDir)
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}

	if len(args) == 0 {
		return runLs(cmd, nil)
	}

	taskState, err := lookupTask(st, args[0])
	if err != nil {
		return err
	}

	if err := restoreTaskIdentity(taskState); err != nil {
		return err
	}

	fmt.Printf("Resuming task: %s\n", taskState.TaskID)
	fmt.Printf("Image: %s\n", taskState.Canonical)
	fmt.Printf("Platform: %s\n", taskState.Platform)
	if taskState.Mirror.Endpoint != "" {
		fmt.Printf("Mirror: %s\n", taskState.Mirror.Endpoint)
	}
	fmt.Printf("Progress: %.1f%%\n\n", taskState.Progress()*100)
	printTaskLayers(taskState)
	fmt.Println()

	continueFlag = true
	return runPull(cmd, []string{taskState.Canonical})
}

// restoreTaskIdentity pins the resume to the task's platform and mirror.
// Recomputing the task id from the host platform would start a different download.
func restoreTaskIdentity(taskState *store.TaskState) error {
	if rootCmd.PersistentFlags().Changed("platform") && platform != taskState.Platform {
		return fmt.Errorf("task %s is platform %s, not %s", taskState.TaskID, taskState.Platform, platform)
	}
	if taskState.Platform != "" {
		platform = taskState.Platform
	}
	if !rootCmd.PersistentFlags().Changed("mirror") && taskState.Mirror.Endpoint != "" {
		mirrorHost = taskState.Mirror.Endpoint
		// Path is already rendered. It has no template variables, so using it
		// as the template keeps the original repository path.
		if taskState.Mirror.Path != "" {
			mirrorPath = taskState.Mirror.Path
		}
	}
	return nil
}
