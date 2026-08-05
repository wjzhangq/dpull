package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wjzhangq/dpull/internal/store"
)

var (
	rmAll bool
)

var rmCmd = &cobra.Command{
	Use:   "rm TASK_ID [TASK_ID...]",
	Short: "Remove task(s) and their cached blobs",
	Long: `Remove one or more download tasks and their associated cached blobs.

This frees up disk space by deleting incomplete downloads.`,
	RunE: runRm,
}

func init() {
	rmCmd.Flags().BoolVar(&rmAll, "all", false, "remove all tasks")
}

func runRm(cmd *cobra.Command, args []string) error {
	// Initialize store
	st, err := store.NewStore(cacheDir)
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}

	var taskIDs []string

	if rmAll {
		// Remove all tasks
		tasks, err := st.ListTasks()
		if err != nil {
			return fmt.Errorf("list tasks: %w", err)
		}

		if len(tasks) == 0 {
			fmt.Println("No tasks to remove.")
			return nil
		}

		for _, task := range tasks {
			taskIDs = append(taskIDs, task.TaskID)
		}
	} else {
		if len(args) == 0 {
			return fmt.Errorf("task ID required (use --all to remove all tasks)")
		}
		taskIDs = args
	}

	// Remove each task
	for _, taskID := range taskIDs {
		if !st.TaskExists(taskID) {
			fmt.Fprintf(cmd.OutOrStderr(), "warn: task not found: %s\n", taskID)
			continue
		}

		taskState, err := store.LoadTask(st.TaskPath(taskID))
		if err != nil {
			fmt.Fprintf(cmd.OutOrStderr(), "warn: load task %s: %v\n", taskID, err)
			continue
		}

		// Remove cached blobs
		for _, blob := range taskState.Blobs {
			if err := st.RemoveBlob(blob.Digest); err != nil {
				fmt.Fprintf(cmd.OutOrStderr(), "warn: remove blob %s: %v\n", blob.Digest, err)
			}
		}

		// Remove config blob
		if err := st.RemoveBlob(taskState.ConfigDigest); err != nil {
			fmt.Fprintf(cmd.OutOrStderr(), "warn: remove config %s: %v\n", taskState.ConfigDigest, err)
		}

		// Remove task state
		if err := st.RemoveTask(taskID); err != nil {
			return fmt.Errorf("remove task %s: %w", taskID, err)
		}

		fmt.Printf("Removed task: %s (%s)\n", taskID, taskState.Canonical)
	}

	return nil
}
