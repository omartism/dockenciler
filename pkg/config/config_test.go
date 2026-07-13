package config

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
    // Test with complete config
    completeConfig := `{
        "registry": {
            "type": "ecr",
            "ecr": {
                "region": "us-west-2",
                "access_key": "test-key",
                "secret_key": "test-secret"
            }
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
    if config.Registry.ECR == nil {
        t.Fatal("Expected ECR config to be non-nil")
    }
    if config.Registry.ECR.Region != "us-west-2" {
        t.Errorf("Expected region 'us-west-2', got '%s'", config.Registry.ECR.Region)
    }
    if config.Registry.ECR.AccessKey != "test-key" {
        t.Errorf("Expected access_key 'test-key', got '%s'", config.Registry.ECR.AccessKey)
    }
    if config.Registry.ECR.SecretKey != "test-secret" {
        t.Errorf("Expected secret_key 'test-secret', got '%s'", config.Registry.ECR.SecretKey)
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
            "ecr": {
                "region": "us-east-1"
            }
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

func TestLoadConfig_GCR(t *testing.T) {
    gcrConfig := `{
        "registry": {
            "type": "gcr",
            "gcr": {
                "auth": {
                    "method": "adc"
                }
            }
        }
    }`

    config, err := LoadConfigFromString(gcrConfig)
    if err != nil {
        t.Fatalf("Failed to load GCR config: %v", err)
    }

    if config.Registry.Type != "gcr" {
        t.Errorf("Expected registry type 'gcr', got '%s'", config.Registry.Type)
    }
    if config.Registry.GCR == nil {
        t.Fatal("Expected GCR config to be non-nil")
    }
    if config.Registry.GCR.Auth.Method != "adc" {
        t.Errorf("Expected GCR auth method 'adc', got '%s'", config.Registry.GCR.Auth.Method)
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

// TestExampleConfigs validates every docs/examples/*.json file by loading each
// into the Config struct. This catches schema drift between documentation
// examples and the real config schema.
//
// Phase 3 of the docs revamp.
func TestExampleConfigs(t *testing.T) {
	type expectation struct {
		file          string
		registryType  string
		needsECR      bool
		needsGCR      bool
	}
	expectations := []expectation{
		{file: "ecr-basic.json", registryType: "ecr", needsECR: true},
		{file: "ecr-imds.json", registryType: "ecr", needsECR: true},
		{file: "swarm-rolling.json", registryType: "ecr", needsECR: true},
		{file: "advanced-matching.json", registryType: "ecr", needsECR: true},
		{file: "gcr-adc.json", registryType: "gcr", needsGCR: true},
		{file: "gcr-service-account.json", registryType: "gcr", needsGCR: true},
		{file: "multi-notifier.json", registryType: "ecr", needsECR: true},
		{file: "dry-run.json", registryType: "ecr", needsECR: true},
	}

	for _, exp := range expectations {
		t.Run(exp.file, func(t *testing.T) {
			path := filepath.Join("..", "..", "docs", "examples", exp.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read example %s: %v", exp.file, err)
			}

			var cfg Config
			if err := json.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("unmarshal %s into Config: %v", exp.file, err)
			}

			assert.Equal(t, exp.registryType, cfg.Registry.Type, "registry.type mismatch in %s", exp.file)

			if exp.needsECR {
				assert.NotNil(t, cfg.Registry.ECR, "registry.ecr must be non-nil for %s", exp.file)
			} else {
				assert.Nil(t, cfg.Registry.ECR, "registry.ecr must be nil for %s", exp.file)
			}

			if exp.needsGCR {
				assert.NotNil(t, cfg.Registry.GCR, "registry.gcr must be non-nil for %s", exp.file)
			} else {
				assert.Nil(t, cfg.Registry.GCR, "registry.gcr must be nil for %s", exp.file)
			}

			// Per-file value assertions — each example must demonstrate its claimed feature.
			switch exp.file {
			case "ecr-imds.json":
				assert.Empty(t, cfg.Registry.ECR.AccessKey, "IMDSv2 example should not have access_key")
				assert.Empty(t, cfg.Registry.ECR.SecretKey, "IMDSv2 example should not have secret_key")
			case "gcr-adc.json":
				assert.Equal(t, "adc", cfg.Registry.GCR.Auth.Method, "GCR ADC example should use method=adc")
				assert.Empty(t, cfg.Registry.GCR.Auth.ServiceAccountFile, "GCR ADC example should not have service_account_file")
			case "gcr-service-account.json":
				assert.Equal(t, "service_account", cfg.Registry.GCR.Auth.Method)
				assert.NotEmpty(t, cfg.Registry.GCR.Auth.ServiceAccountFile, "GCR service-account example must include service_account_file path")
			case "advanced-matching.json":
				assert.NotEmpty(t, cfg.Criteria.Version, "advanced-matching example must set criteria.version")
				assert.Equal(t, 2, len(cfg.Exclusions), "advanced-matching example must have 2 exclusion entries")
			case "multi-notifier.json":
				assert.NotEmpty(t, cfg.Notifications.SlackWebhookURL, "multi-notifier example must set slack_webhook_url")
				assert.NotEmpty(t, cfg.Notifications.TelegramBotToken, "multi-notifier example must set telegram_bot_token")
				assert.NotEmpty(t, cfg.Notifications.EmailHost, "multi-notifier example must set email_host")
			case "dry-run.json":
				assert.True(t, cfg.DryRun, "dry-run example must set dry_run=true")
			case "swarm-rolling.json":
				assert.NotEmpty(t, cfg.Notifications.SlackWebhookURL, "swarm-rolling example must set slack_webhook_url")
			}
		})
	}
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