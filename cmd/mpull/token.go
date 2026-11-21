package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wjzhangq/dpull/internal/model"
)

func resolveToken(kind model.Kind, flag string) string {
	flag = strings.TrimSpace(flag)
	if flag != "" {
		return flag
	}
	if kind == model.KindModelScope {
		if value := os.Getenv("MODELSCOPE_API_TOKEN"); value != "" {
			return value
		}
		return readModelScopeToken()
	}
	if value := os.Getenv("HF_TOKEN"); value != "" {
		return value
	}
	if value := os.Getenv("HUGGING_FACE_HUB_TOKEN"); value != "" {
		return value
	}
	return readHFToken()
}

func readHFToken() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	body, err := os.ReadFile(filepath.Join(home, ".cache", "huggingface", "token"))
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(body), "\n")
	return strings.TrimSpace(line)
}

func readModelScopeToken() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	body, err := os.ReadFile(filepath.Join(home, ".modelscope", "credentials"))
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}
	if !strings.Contains(text, "=") && !strings.Contains(text, "[") && !strings.Contains(text, "\n") {
		return text
	}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "access_token", "token":
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}
