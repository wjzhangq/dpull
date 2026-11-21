package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wjzhangq/dpull/pkg/version"
)

var (
	outputDir      string
	revision       string
	mirrorName     string
	token          string
	includes       []string
	excludes       []string
	cacheDir       string
	proxyURL       string
	jobs           int
	conns          int
	minSplit       int64
	maxRetries     int
	progressMode   string
	force          bool
	checkIntegrity bool
)

var rootCmd = &cobra.Command{
	Use:   "mpull [flags] MODEL",
	Short: "Download a model repository with multi-connection resume",
	Long: `mpull downloads model files with the same byte-level resume used by dpull.

A bare org/name refers to Hugging Face. Without a proxy the request goes to
hf-mirror.com; with a proxy or -m huggingface it goes to huggingface.co.
ModelScope is a different repository: use ms:org/name.

Examples:
  mpull Qwen/Qwen2.5-7B-Instruct
  mpull -m huggingface meta-llama/Llama-3.1-8B-Instruct
  mpull ms:Qwen/Qwen2.5-7B-Instruct
  mpull info Qwen/Qwen2.5-7B-Instruct
  mpull -o ./models/qwen --include '*.safetensors' --include '*.json' Qwen/Qwen2.5-7B-Instruct`,
	Version:      version.String(),
	SilenceUsage: true,
	RunE:         runPull,
	Args:         cobra.ExactArgs(1),
}

func init() {
	home, _ := os.UserHomeDir()
	defaultCache := filepath.Join(home, ".mpull", "cache")

	rootCmd.PersistentFlags().StringVarP(&outputDir, "output", "o", "", "output directory (default: ./<model-name>)")
	rootCmd.PersistentFlags().StringVar(&revision, "revision", "", "git revision (Hugging Face default main, ModelScope default master)")
	rootCmd.PersistentFlags().StringVarP(&mirrorName, "mirror", "m", "", "hf-mirror, huggingface, or modelscope")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "hub token (default: HF_TOKEN or MODELSCOPE_API_TOKEN)")
	rootCmd.PersistentFlags().StringArrayVar(&includes, "include", nil, "glob of files to keep; repeat for more than one")
	rootCmd.PersistentFlags().StringArrayVar(&excludes, "exclude", nil, "glob of files to skip; repeat for more than one")
	rootCmd.PersistentFlags().StringVar(&cacheDir, "cache-dir", defaultCache, "cache directory for blobs and state")
	rootCmd.PersistentFlags().StringVar(&proxyURL, "proxy", "", "HTTP proxy URL (overrides HTTP_PROXY and HTTPS_PROXY)")
	rootCmd.PersistentFlags().IntVarP(&jobs, "jobs", "j", 3, "max concurrent file downloads")
	rootCmd.PersistentFlags().IntVarP(&conns, "connections", "x", 8, "connections per file")
	rootCmd.PersistentFlags().Int64Var(&minSplit, "min-split-size", 20*1024*1024, "minimum file size to split, in bytes")
	rootCmd.PersistentFlags().IntVar(&maxRetries, "max-retries", 10, "max retries per piece")
	rootCmd.PersistentFlags().StringVar(&progressMode, "progress", "bar", "progress mode: bar, plain, json, none")
	rootCmd.PersistentFlags().BoolVar(&force, "force", false, "restart even when the saved task no longer matches")
	rootCmd.PersistentFlags().BoolVar(&checkIntegrity, "check-integrity", true, "verify sha256 when the hub provides one")

	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(mirrorsCmd)
}

func effectiveProxy() string {
	if proxyURL != "" {
		return proxyURL
	}
	for _, key := range []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy"} {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func mirrorsCmdRun(cmd *cobra.Command, args []string) error {
	fmt.Print(`hf-mirror     hf-mirror.com       Hugging Face mirror; default when no proxy is set
huggingface   huggingface.co      Official Hub; default when a proxy is set
modelscope    www.modelscope.cn   Separate repository; pass ms:org/name
`)
	return nil
}

var mirrorsCmd = &cobra.Command{
	Use:   "mirrors",
	Short: "List model hubs",
	Args:  cobra.NoArgs,
	RunE:  mirrorsCmdRun,
}
