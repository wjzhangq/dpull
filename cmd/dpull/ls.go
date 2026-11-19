package main

import (
	"fmt"

	"github.com/docker/go-units"
	"github.com/spf13/cobra"
	"github.com/wjzhangq/dpull/internal/store"
)

var lsCmd = &cobra.Command{
	Use:   "ls [TASK_ID]",
	Short: "List download tasks, or every layer of one task",
	Args:  cobra.MaximumNArgs(1),
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

	if len(args) == 1 {
		task, err := lookupTask(st, args[0])
		if err != nil {
			return err
		}
		printTaskLayers(task)
		return nil
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found.")
		return nil
	}

	fmt.Printf("%-18s  %-40s  %10s  %8s  %8s  %s\n", "TASK ID", "IMAGE", "SIZE", "LAYERS", "PROGRESS", "UPDATED")
	fmt.Println("--------------------------------------------------------------------------------------------------------")

	for _, task := range tasks {
		done := 0
		for _, blob := range task.Blobs {
			if blob.State == "complete" && blob.Verified {
				done++
			}
		}

		imageName := task.Canonical
		if len(imageName) > 40 {
			imageName = imageName[:37] + "..."
		}

		fmt.Printf("%-18s  %-40s  %10s  %8s  %7.1f%%  %s\n",
			task.TaskID,
			imageName,
			units.HumanSize(float64(task.TotalSize)),
			fmt.Sprintf("%d/%d", done, len(task.Blobs)),
			task.Progress()*100,
			task.UpdatedAt.Format("2006-01-02 15:04"),
		)
	}

	return nil
}

func printTaskLayers(task *store.TaskState) {
	fmt.Printf("Task:     %s\n", task.TaskID)
	fmt.Printf("Image:    %s\n", task.Canonical)
	fmt.Printf("Platform: %s\n", task.Platform)
	if task.Mirror.Endpoint != "" {
		fmt.Printf("Mirror:   %s\n", task.Mirror.Endpoint)
	}
	fmt.Printf("Progress: %.1f%%\n", task.Progress()*100)
	fmt.Printf("Layers:   %d\n\n", len(task.Blobs))

	if len(task.Blobs) == 0 {
		fmt.Println("No layers recorded yet.")
		return
	}

	fmt.Printf("%3s  %-71s  %10s  %s\n", "#", "DIGEST", "SIZE", "STATUS")
	for i, blob := range task.Blobs {
		fmt.Printf("%3d  %-71s  %10s  %s\n", i+1, blob.Digest, units.HumanSize(float64(blob.Size)), layerStatus(blob))
	}
}

func layerStatus(blob store.BlobState) string {
	if blob.State == "complete" && blob.Verified {
		return "complete"
	}
	if blob.Pieces > 0 {
		done := 0
		if blob.Bitfield != "" {
			bf := store.NewBitfield(blob.Pieces)
			if err := bf.FromHex(blob.Bitfield); err == nil {
				done = bf.Count()
			}
		}
		got := units.HumanSize(float64(blob.BytesDownloaded()))
		return fmt.Sprintf("%s %s (%d/%d pieces)", blob.State, got, done, blob.Pieces)
	}
	if blob.State == "" {
		return "pending"
	}
	return blob.State
}

func lookupTask(st *store.Store, key string) (*store.TaskState, error) {
	if st.TaskExists(key) {
		task, err := store.LoadTask(st.TaskPath(key))
		if err != nil {
			return nil, fmt.Errorf("load task: %w", err)
		}
		return task, nil
	}

	tasks, err := st.ListTasks()
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	for _, task := range tasks {
		if task.Canonical == key || task.TaskID == key {
			return task, nil
		}
	}
	return nil, fmt.Errorf("task not found: %s", key)
}
