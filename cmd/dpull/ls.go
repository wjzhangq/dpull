package main

import (
	"fmt"

	"github.com/docker/go-units"
	"github.com/spf13/cobra"
	"github.com/wjzhangq/dpull/internal/store"
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List unfinished download tasks",
	RunE:  runLs,
}

func runLs(cmd *cobra.Command, args []string) error {
	// Initialize store
	st, err := store.NewStore(cacheDir)
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}

	// List all tasks
	tasks, err := st.ListTasks()
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found.")
		return nil
	}

	// Print header
	fmt.Printf("%-18s  %-45s  %10s  %8s  %s\n", "TASK ID", "IMAGE", "SIZE", "PROGRESS", "UPDATED")
	fmt.Println("--------------------------------------------------------------------------------")

	// Print each task
	for _, task := range tasks {
		// Calculate progress
		completed := 0
		for _, blob := range task.Blobs {
			if blob.State == "complete" && blob.Verified {
				completed++
			}
		}

		progressPct := task.Progress() * 100

		// Truncate image name if too long
		imageName := task.Canonical
		if len(imageName) > 45 {
			imageName = imageName[:42] + "..."
		}

		// Format updated time
		updated := task.UpdatedAt.Format("2006-01-02 15:04")

		// Format total size
		sizeStr := units.HumanSize(float64(task.TotalSize))

		fmt.Printf("%-18s  %-45s  %10s  %6.1f%%  %s\n",
			task.TaskID,
			imageName,
			sizeStr,
			progressPct,
			updated,
		)
	}

	return nil
}
