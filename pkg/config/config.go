package config

import (
    "fmt"
    "log/slog"
    "os"
    "strings"

    "github.com/spf13/viper"
)

type Registry struct {
    Type       string `json:"type" mapstructure:"type"`
    Region     string `json:"region" mapstructure:"region"`
    AccessKey  string `json:"access_key" mapstructure:"access_key"`
    SecretKey  string `json:"secret_key" mapstructure:"secret_key"`
}

type Docker struct {
    SocketPath   string `json:"socket_path" mapstructure:"socket_path"`
    LabelFilter  string `json:"label_filter" mapstructure:"label_filter"`
}

type Criteria struct {
    Version string `json:"version" mapstructure:"version"`
    Regex   string `json:"regex" mapstructure:"regex"`
    Digest  string `json:"digest" mapstructure:"digest"`
}

type Config struct {
    Registry         Registry `json:"registry" mapstructure:"registry"`
    Docker           Docker   `json:"docker" mapstructure:"docker"`
    ReconcileInterval string   `json:"reconcile_interval" mapstructure:"reconcile_interval"`
    LogLevel         string   `json:"log_level" mapstructure:"log_level"`
    Criteria         Criteria `json:"criteria" mapstructure:"criteria"`
    DryRun           bool     `json:"dry_run" mapstructure:"dry_run"`
    Exclusions       []string `json:"exclusions" mapstructure:"exclusions"`
}

func LoadConfig(path string) (*Config, error) {
    // Create a new viper instance
    v := viper.New()
    
    // Set config file name and type
    v.SetConfigFile(path)
    v.SetConfigType("json")
    
    // Enable environment variable overrides with DOCKENCILER prefix
    v.SetEnvPrefix("DOCKENCILER")
    v.AutomaticEnv()

    // Set defaults so AutomaticEnv knows which keys to look for
    v.SetDefault("registry.type", "")
    v.SetDefault("registry.region", "")
    v.SetDefault("registry.access_key", "")
    v.SetDefault("registry.secret_key", "")
    v.SetDefault("docker.socket_path", "/var/run/docker.sock")
    v.SetDefault("docker.label_filter", "dockenciler.autoupdate=true")
    v.SetDefault("reconcile_interval", "1h")
    v.SetDefault("log_level", "info")
    v.SetDefault("dry_run", false)

    // Handle nested structs by replacing dots with underscores
    v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
    
    // Read config file
    if err := v.ReadInConfig(); err != nil {
        // If config file doesn't exist, that's OK - we'll use defaults
        if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
            return nil, fmt.Errorf("failed to read config file: %w", err)
        }
    }
    
    // Unmarshal config into struct
    var config Config
    if err := v.Unmarshal(&config); err != nil {
        return nil, fmt.Errorf("failed to unmarshal config: %w", err)
    }

    return &config, nil
}

func SetupLogging(level string) {
    // Map the level string (case-insensitive) to slog.Level
    var slogLevel slog.Level
    switch strings.ToLower(level) {
    case "debug":
        slogLevel = slog.LevelDebug
    case "info":
        slogLevel = slog.LevelInfo
    case "warn":
        slogLevel = slog.LevelWarn
    case "error":
        slogLevel = slog.LevelError
    default:
        // Default to slog.LevelInfo if the level is unknown or empty
        slogLevel = slog.LevelInfo
    }

    // Set the global logger
    slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})))
}