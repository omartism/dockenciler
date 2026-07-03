package config

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type ECRConfig struct {
	Region    string `json:"region" mapstructure:"region"`
	AccessKey string `json:"access_key" mapstructure:"access_key"`
	SecretKey string `json:"secret_key" mapstructure:"secret_key"`
}

type GCRAuth struct {
	Method             string `json:"method" mapstructure:"method"`                       // "adc" | "service_account"
	ServiceAccountFile string `json:"service_account_file" mapstructure:"service_account_file"`
}

type GCRConfig struct {
	Auth GCRAuth `json:"auth" mapstructure:"auth"`
}

type Registry struct {
	Type string      `json:"type" mapstructure:"type"` // "ecr" | "gcr"
	ECR  *ECRConfig  `json:"ecr,omitempty" mapstructure:"ecr"`
	GCR  *GCRConfig  `json:"gcr,omitempty" mapstructure:"gcr"`
}

type Docker struct {
	SocketPath  string `json:"socket_path" mapstructure:"socket_path"`
	LabelFilter string `json:"label_filter" mapstructure:"label_filter"`
}

type Criteria struct {
	Version string `json:"version" mapstructure:"version"`
	Regex   string `json:"regex" mapstructure:"regex"`
	Digest  string `json:"digest" mapstructure:"digest"`
}

type Config struct {
	Registry          Registry      `json:"registry" mapstructure:"registry"`
	Docker            Docker        `json:"docker" mapstructure:"docker"`
	Notifications     Notifications `json:"notifications" mapstructure:"notifications"`
	ReconcileInterval string        `json:"reconcile_interval" mapstructure:"reconcile_interval"`
	LogLevel          string        `json:"log_level" mapstructure:"log_level"`
	Criteria          Criteria      `json:"criteria" mapstructure:"criteria"`
	ColorLogs         bool          `json:"color_logs" mapstructure:"color_logs"`
	DryRun            bool          `json:"dry_run" mapstructure:"dry_run"`
	Exclusions        []string      `json:"exclusions" mapstructure:"exclusions"`
	Timezone          string        `json:"timezone" mapstructure:"timezone"`
}

type Notifications struct {
	SlackWebhookURL      string                `json:"slack_webhook_url" mapstructure:"slack_webhook_url"`
	DiscordWebhookURL    string                `json:"discord_webhook_url" mapstructure:"discord_webhook_url"`
	TelegramBotToken     string                `json:"telegram_bot_token" mapstructure:"telegram_bot_token"`
	TelegramChatID       string                `json:"telegram_chat_id" mapstructure:"telegram_chat_id"`
	EmailHost            string                `json:"email_host" mapstructure:"email_host"`
	EmailPort            string                `json:"email_port" mapstructure:"email_port"`
	EmailUser            string                `json:"email_user" mapstructure:"email_user"`
	EmailPassword        string                `json:"email_password" mapstructure:"email_password"`
	EmailFrom            string                `json:"email_from" mapstructure:"email_from"`
	EmailTo              string                `json:"email_to" mapstructure:"email_to"`
	MSTeamsWebhookURL    string                `json:"msteams_webhook_url" mapstructure:"msteams_webhook_url"`
	GoogleChatWebhookURL string                `json:"google_chat_webhook_url" mapstructure:"google_chat_webhook_url"`
	Templates            NotificationTemplates `json:"templates" mapstructure:"templates"`
}

type NotificationTemplates struct {
	Default    string `json:"default"     mapstructure:"default"`
	Slack      string `json:"slack"       mapstructure:"slack"`
	Discord    string `json:"discord"     mapstructure:"discord"`
	Telegram   string `json:"telegram"    mapstructure:"telegram"`
	Email      string `json:"email"       mapstructure:"email"`
	MSTeams    string `json:"msteams"     mapstructure:"msteams"`
	GoogleChat string `json:"google_chat" mapstructure:"google_chat"`
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
	v.SetDefault("registry.ecr.region", "")
	v.SetDefault("registry.ecr.access_key", "")
	v.SetDefault("registry.ecr.secret_key", "")
	v.SetDefault("registry.gcr.auth.method", "adc")
	v.SetDefault("registry.gcr.auth.service_account_file", "")
	v.SetDefault("docker.socket_path", "/var/run/docker.sock")
	v.SetDefault("docker.label_filter", "dockenciler.autoupdate=true")
	v.SetDefault("reconcile_interval", "1h")
	v.SetDefault("log_level", "info")
	v.SetDefault("dry_run", false)
	v.SetDefault("color_logs", true)
	v.SetDefault("notifications.slack_webhook_url", "")
	v.SetDefault("notifications.discord_webhook_url", "")
	v.SetDefault("notifications.telegram_bot_token", "")
	v.SetDefault("notifications.telegram_chat_id", "")
	v.SetDefault("notifications.email_host", "")
	v.SetDefault("notifications.email_port", "")
	v.SetDefault("notifications.email_user", "")
	v.SetDefault("notifications.email_password", "")
	v.SetDefault("notifications.email_from", "")
	v.SetDefault("notifications.email_to", "")
	v.SetDefault("notifications.msteams_webhook_url", "")
	v.SetDefault("notifications.google_chat_webhook_url", "")
	v.SetDefault("notifications.templates.default", "")
	v.SetDefault("notifications.templates.slack", "")
	v.SetDefault("notifications.templates.discord", "")
	v.SetDefault("notifications.templates.telegram", "")
	v.SetDefault("notifications.templates.email", "")
	v.SetDefault("notifications.templates.msteams", "")
	v.SetDefault("notifications.templates.google_chat", "")
	v.SetDefault("timezone", "Host")

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

func SetupLogging(level string, colorLogs bool) {
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

	// Set the global logger with optional colorized output
	var out io.Writer = os.Stdout
	if colorLogs {
		out = NewColorWriter(os.Stdout)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slogLevel})))
}

// ResolveTimezone resolves a timezone string to a *time.Location.
// "Host" or empty returns time.Local (system timezone).
// Any other string is treated as an IANA timezone name.
func ResolveTimezone(tz string) (*time.Location, error) {
	if tz == "" || tz == "Host" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	return loc, nil
}
