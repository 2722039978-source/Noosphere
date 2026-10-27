package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	App      AppSettings      `yaml:"app"`
	Server   ServerSettings   `yaml:"server"`
	Modules  ModuleSettings   `yaml:"modules"`
	Gateway  GatewaySettings  `yaml:"gateway"`
	Logging  LogSettings      `yaml:"logging"`
	ModelLab ModelLabSettings `yaml:"model_lab"`
}

// ModelLab is local offline execution only; it never enables live routing.
type ModelLabSettings struct {
	Enabled                 bool   `yaml:"enabled"`
	GoBinary                string `yaml:"go_binary"`
	MaxContext              int64  `yaml:"max_context"`
	MaxOutput               int64  `yaml:"max_output"`
	InferenceTimeoutSeconds int64  `yaml:"inference_timeout_seconds"`
	CleanupTimeoutSeconds   int64  `yaml:"cleanup_timeout_seconds"`
}

type AppSettings struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	DataDir string `yaml:"data_dir"`
}

type ServerSettings struct {
	Host           string   `yaml:"host"`
	Port           int      `yaml:"port"`
	AllowedOrigins []string `yaml:"allowed_origins"`
}

type ModuleSettings struct {
	Nebula   NebulaConfig   `yaml:"nebula"`
	DevOps   DevOpsConfig   `yaml:"devops"`
	CodeLens CodeLensConfig `yaml:"codelens"`
}

type NebulaConfig struct {
	Enabled   bool            `yaml:"enabled"`
	DataDir   string          `yaml:"data_dir"`
	Embedding EmbeddingConfig `yaml:"embedding"`
}

type EmbeddingConfig struct {
	Provider string `yaml:"provider"` // openai, ollama, mock
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
	Dim      int    `yaml:"dim"`
}

type DevOpsConfig struct {
	Enabled bool `yaml:"enabled"`
}

type CodeLensConfig struct {
	Enabled    bool   `yaml:"enabled"`
	PythonPath string `yaml:"python_path"`
	Port       int    `yaml:"port"`
}

type GatewaySettings struct {
	ConfigPath string `yaml:"config_path"`
}

type LogSettings struct {
	Dir   string `yaml:"dir"`
	Level string `yaml:"level"`
	MaxMB int    `yaml:"max_mb"`
}

func Load(path string) (*AppConfig, error) {
	cfg := defaults()

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	return cfg, nil
}

func defaults() *AppConfig {
	return &AppConfig{
		ModelLab: ModelLabSettings{Enabled: true, MaxContext: 8192, MaxOutput: 2048, InferenceTimeoutSeconds: 120, CleanupTimeoutSeconds: 10},
		App: AppSettings{
			Name:    "Noosphere",
			Version: "0.5.0",
			DataDir: "./data",
		},
		Server: ServerSettings{
			Host: "127.0.0.1",
			Port: 8080,
			AllowedOrigins: []string{
				"http://localhost:1420",
				"http://127.0.0.1:1420",
				"http://tauri.localhost",
				"tauri://localhost",
			},
		},
		Modules: ModuleSettings{
			Nebula: NebulaConfig{
				Enabled: true,
				DataDir: "./data/nebula",
				Embedding: EmbeddingConfig{
					Provider: "openai",
					Model:    "text-embedding-3-small",
					Dim:      1536,
				},
			},
			DevOps:   DevOpsConfig{Enabled: true},
			CodeLens: CodeLensConfig{Enabled: true, PythonPath: "python", Port: 18765},
		},
		Gateway: GatewaySettings{
			ConfigPath: "./models.yaml",
		},
		Logging: LogSettings{
			Dir:   "./logs",
			Level: "info",
			MaxMB: 10,
		},
	}
}
