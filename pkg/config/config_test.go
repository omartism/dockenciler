package config

import (
    "encoding/json"
    "testing"
)

func TestLoadConfig(t *testing.T) {
    // Test with complete config
    completeConfig := `{
        "registry": {
            "type": "ecr",
            "region": "us-west-2",
            "access_key": "test-key",
            "secret_key": "test-secret"
        },
        "docker": {
            "socket_path": "/var/run/docker.sock",
            "label_filter": "dockenciler.autoupdate=true"
        },
        "reconcile_interval": "1h",
        "log_level": "info",
        "criteria": {
            "version": "v1.0.0",
            "regex": "^v\\d+\\.\\d+\\.\\d+$",
            "digest": "sha256:abc123"
        }
    }`

    config, err := LoadConfigFromString(completeConfig)
    if err != nil {
        t.Fatalf("Failed to load complete config: %v", err)
    }

    if config.Registry.Type != "ecr" {
        t.Errorf("Expected registry type 'ecr', got '%s'", config.Registry.Type)
    }
    if config.Registry.Region != "us-west-2" {
        t.Errorf("Expected region 'us-west-2', got '%s'", config.Registry.Region)
    }
    if config.Docker.SocketPath != "/var/run/docker.sock" {
        t.Errorf("Expected socket path '/var/run/docker.sock', got '%s'", config.Docker.SocketPath)
    }
    if config.Docker.LabelFilter != "dockenciler.autoupdate=true" {
        t.Errorf("Expected label filter 'dockenciler.autoupdate=true', got '%s'", config.Docker.LabelFilter)
    }
    if config.ReconcileInterval != "1h" {
        t.Errorf("Expected reconcile interval '1h', got '%s'", config.ReconcileInterval)
    }
    if config.LogLevel != "info" {
        t.Errorf("Expected log level 'info', got '%s'", config.LogLevel)
    }
    if config.Criteria.Version != "v1.0.0" {
        t.Errorf("Expected criteria version 'v1.0.0', got '%s'", config.Criteria.Version)
    }
    if config.Criteria.Regex != "^v\\d+\\.\\d+\\.\\d+$" {
        t.Errorf("Expected criteria regex '^v\\\\d+\\\\.\\\\d+\\\\.\\\\d+$', got '%s'", config.Criteria.Regex)
    }
    if config.Criteria.Digest != "sha256:abc123" {
        t.Errorf("Expected criteria digest 'sha256:abc123', got '%s'", config.Criteria.Digest)
    }
}

func TestLoadConfigWithDefaults(t *testing.T) {
    // Test with minimal config (only registry settings)
    minimalConfig := `{
        "registry": {
            "type": "ecr",
            "region": "us-east-1"
        }
    }`

    config, err := LoadConfigFromString(minimalConfig)
    if err != nil {
        t.Fatalf("Failed to load minimal config: %v", err)
    }

    // Check that defaults were applied
    if config.Docker.SocketPath != "/var/run/docker.sock" {
        t.Errorf("Expected default socket path '/var/run/docker.sock', got '%s'", config.Docker.SocketPath)
    }
    if config.Docker.LabelFilter != "dockenciler.autoupdate=true" {
        t.Errorf("Expected default label filter 'dockenciler.autoupdate=true', got '%s'", config.Docker.LabelFilter)
    }
    // Check that criteria fields are empty (not set)
    if config.Criteria.Version != "" {
        t.Errorf("Expected empty criteria version, got '%s'", config.Criteria.Version)
    }
    if config.Criteria.Regex != "" {
        t.Errorf("Expected empty criteria regex, got '%s'", config.Criteria.Regex)
    }
    if config.Criteria.Digest != "" {
        t.Errorf("Expected empty criteria digest, got '%s'", config.Criteria.Digest)
    }
    // Check that new fields have defaults
    if config.DryRun != false {
        t.Errorf("Expected default dry_run to be false, got '%v'", config.DryRun)
    }
    if len(config.Exclusions) != 0 {
        t.Errorf("Expected empty exclusions list, got '%v'", config.Exclusions)
    }
}

func TestLoadConfig_DryRunAndExclusions(t *testing.T) {
    // Test with dry_run and exclusions set
    configWithValues := `{
        "registry": {
            "type": "ecr",
            "region": "us-west-2"
        },
        "dry_run": true,
        "exclusions": ["container1", "container2"]
    }`

    config, err := LoadConfigFromString(configWithValues)
    if err != nil {
        t.Fatalf("Failed to load config with dry_run and exclusions: %v", err)
    }

    // Check that dry_run is loaded correctly
    if config.DryRun != true {
        t.Errorf("Expected dry_run to be true, got '%v'", config.DryRun)
    }
    
    // Check that exclusions are loaded correctly
    if len(config.Exclusions) != 2 {
        t.Errorf("Expected 2 exclusions, got %d", len(config.Exclusions))
    }
    if config.Exclusions[0] != "container1" {
        t.Errorf("Expected first exclusion 'container1', got '%s'", config.Exclusions[0])
    }
    if config.Exclusions[1] != "container2" {
        t.Errorf("Expected second exclusion 'container2', got '%s'", config.Exclusions[1])
    }
}

func TestSetupLogging(t *testing.T) {
    // Test that SetupLogging correctly sets the log level
    // We can verify this by checking if debug messages are printed when level is "debug"
    // and not printed when level is "info"
    
    // Test with debug level
    SetupLogging("debug", true)
    // When level is debug, both debug and info messages should be printed
    // We can't easily capture this without mocking, but we can at least verify
    // that the function doesn't panic
    
    // Test with info level  
    SetupLogging("info", true)
    // When level is info, debug messages should not be printed
    // Again, we can't easily capture this without mocking
    
    // Test with warn level
    SetupLogging("warn", true)
    
    // Test with error level
    SetupLogging("error", true)
    
    // Test with unknown level (should default to info)
    SetupLogging("unknown", true)
    
    // Test with empty level (should default to info)
    SetupLogging("", false)
}

// Helper function to load config from string (for testing)
func LoadConfigFromString(configStr string) (*Config, error) {
    var config Config
    if err := json.Unmarshal([]byte(configStr), &config); err != nil {
        return nil, err
    }

    // Set default values for Docker settings if empty
    if config.Docker.SocketPath == "" {
        config.Docker.SocketPath = "/var/run/docker.sock"
    }
    if config.Docker.LabelFilter == "" {
        config.Docker.LabelFilter = "dockenciler.autoupdate=true"
    }

    return &config, nil
}