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
		// No task ID provided, list tasks
		return runLs(cmd, args)
	}

	taskID := args[0]

	// Check if it's an actual task ID or a canonical name
	var taskState *store.TaskState
	if st.TaskExists(taskID) {
		taskState, err = store.LoadTask(st.TaskPath(taskID))
		if err != nil {
			return fmt.Errorf("load task: %w", err)
		}
	} else {
		// Try to find task by canonical name
		tasks, err := st.ListTasks()
		if err != nil {
			return fmt.Errorf("list tasks: %w", err)
		}

		for _, task := range tasks {
			if task.Canonical == taskID {
				taskState = task
				break
			}
		}

		if taskState == nil {
			return fmt.Errorf("task not found: %s", taskID)
		}
	}

	fmt.Printf("Resuming task: %s\n", taskState.TaskID)
	fmt.Printf("Image: %s\n", taskState.Canonical)
	fmt.Printf("Platform: %s\n", taskState.Platform)
	fmt.Printf("Progress: %.1f%%\n\n", taskState.Progress()*100)

	// Resume by calling pull with the canonical name
	// Set continueFlag to ensure we resume existing task
	continueFlag = true
	return runPull(cmd, []string{taskState.Canonical})
}
