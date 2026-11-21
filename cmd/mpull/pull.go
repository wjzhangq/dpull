package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wjzhangq/dpull/internal/model"
)

func runPull(cmd *cobra.Command, args []string) error {
	opt, err := makeOptions(args[0])
	if err != nil {
		return err
	}
	opt.Stdout = os.Stdout
	return model.Pull(cmd.Context(), opt)
}

var infoCmd = &cobra.Command{
	Use:   "info MODEL",
	Short: "Show model files without downloading",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		opt, err := makeOptions(args[0])
		if err != nil {
			return err
		}
		opt.Stdout = os.Stdout
		text, err := model.Info(cmd.Context(), opt)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, text)
		return nil
	},
}

func makeOptions(ref string) (model.PullOptions, error) {
	parsed, err := model.Parse(ref)
	if err != nil {
		return model.PullOptions{}, err
	}
	kind := parsed.Kind
	if mirrorName != "" {
		switch mirrorName {
		case "modelscope", "ms", "modelscope.cn", "www.modelscope.cn":
			kind = model.KindModelScope
		default:
			kind = model.KindHF
		}
	}
	return model.PullOptions{
		Ref:            ref,
		Output:         outputDir,
		Revision:       revision,
		Mirror:         mirrorName,
		Proxy:          effectiveProxy(),
		Token:          resolveToken(kind, token),
		Includes:       includes,
		Excludes:       excludes,
		CacheDir:       cacheDir,
		Jobs:           jobs,
		Connections:    conns,
		MinSplit:       minSplit,
		MaxRetries:     maxRetries,
		Progress:       progressMode,
		Force:          force,
		CheckIntegrity: checkIntegrity,
	}, nil
}
