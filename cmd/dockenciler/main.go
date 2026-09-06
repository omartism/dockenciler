package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/omarismael/dockenciler/pkg/config"
	"github.com/omarismael/dockenciler/pkg/docker"
	"github.com/omarismael/dockenciler/pkg/notifier"
	"github.com/omarismael/dockenciler/pkg/reconciler"
	"github.com/omarismael/dockenciler/pkg/registry"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfgPath := getConfigPath()
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	config.SetupLogging(cfg.LogLevel, cfg.ColorLogs)

	loc, err := config.ResolveTimezone(cfg.Timezone)
	if err != nil {
		slog.Error("Invalid timezone", "timezone", cfg.Timezone, "error", err)
		os.Exit(1)
	}

	printBanner()

	dockerClient, err := docker.NewDockerClient()
	if err != nil {
		slog.Error("Failed to create Docker client", "error", err)
		os.Exit(1)
	}

	var reg registry.Registry
	switch cfg.Registry.Type {
	case "ecr":
		ecrProvider, err := newECRProvider(ctx, cfg)
		if err != nil {
			slog.Error("Failed to create ECR provider", "error", err)
			os.Exit(1)
		}
		reg = ecrProvider
	case "gcr":
		gcrProvider, err := newGCRProvider(ctx, cfg)
		if err != nil {
			slog.Error("Failed to create GCR provider", "error", err)
			os.Exit(1)
		}
		reg = gcrProvider
	case "dockerhub":
		dhCfg := registry.DockerHubConfig{}
		if cfg.Registry.DockerHub != nil {
			dhCfg.Username = cfg.Registry.DockerHub.Username
			dhCfg.Password = cfg.Registry.DockerHub.Password
			dhCfg.ConfigPath = cfg.Registry.DockerHub.ConfigPath
		}
		dhProvider := registry.NewDockerHubProvider(&http.Client{Timeout: 30 * time.Second}, dhCfg)
		reg = dhProvider
		if dhProvider.HasCredentials() {
			slog.Info("Docker Hub registry provider initialized (authenticated access)")
		} else {
			slog.Info("Docker Hub registry provider initialized (anonymous access)")
		}
	case "ghcr":
		ghProvider, err := newGHCRProvider(ctx, cfg)
		if err != nil {
			slog.Error("Failed to create GHCR provider", "error", err)
			os.Exit(1)
		}
		reg = ghProvider
		if ghProvider.HasCredentials() {
			slog.Info("GHCR registry provider initialized (authenticated access)")
		} else {
			slog.Info("GHCR registry provider initialized (anonymous access)")
		}
	case "all":
		router, names := newRouter(ctx, cfg)
		if router == nil {
			slog.Error("Failed to build ECR/GCR providers for multi-registry router")
			os.Exit(1)
		}
		reg = router
		slog.Info("Multi-registry router initialized", "providers", names)
	default:
		slog.Error("Unsupported registry type", "type", cfg.Registry.Type)
		os.Exit(1)
	}

	notif := newNotifier(cfg)

	r := &reconciler.Reconciler{
		DockerClient: dockerClient,
		Registry:     reg,
		Notifier:     notif,
		Config:       cfg,
		Location:     loc,
	}

	interval, err := time.ParseDuration(cfg.ReconcileInterval)
	if err != nil {
		slog.Error("Invalid reconcile interval", "interval", cfg.ReconcileInterval, "error", err)
		os.Exit(1)
	}

	slog.Info("Starting dockenciler", "interval", interval, "label_filter", cfg.Docker.LabelFilter, "timezone", cfg.Timezone)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if err := r.Reconcile(ctx); err != nil {
		slog.Error("Initial reconciliation failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutdown signal received")
			return
		case <-ticker.C:
			if err := r.Reconcile(ctx); err != nil {
				slog.Error("Reconciliation failed", "error", err)
			}
		}
	}
}

func getConfigPath() string {
	if len(os.Args) > 1 {
		return os.Args[1]
	}
	return ""
}

func printBanner() {
	banner := `
██████   ██████   ██████ ██   ██ ███████ ███    ██  ██████ ██ ██      ███████ ██████  
██   ██ ██    ██ ██      ██  ██  ██      ████   ██ ██      ██ ██      ██      ██   ██ 
██   ██ ██    ██ ██      █████   █████   ██ ██  ██ ██      ██ ██      █████   ██████  
██   ██ ██    ██ ██      ██  ██  ██      ██  ██ ██ ██      ██ ██      ██      ██   ██ 
██████   ██████   ██████ ██   ██ ███████ ██   ████  ██████ ██ ███████ ███████ ██   ██ 
`
	fmt.Print(banner)
	slog.Info("Dockenciler started", "version", "alpha")
}

func newECRProvider(ctx context.Context, cfg *config.Config) (*registry.ECRProvider, error) {
	if cfg.Registry.ECR == nil {
		return nil, fmt.Errorf("ECR registry type requires ecr configuration")
	}

	awsCfg, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion(cfg.Registry.ECR.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	if cfg.Registry.ECR.AccessKey != "" && cfg.Registry.ECR.SecretKey != "" {
		awsCfg.Credentials = aws.NewCredentialsCache(
			aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
				return aws.Credentials{
					AccessKeyID:     cfg.Registry.ECR.AccessKey,
					SecretAccessKey: cfg.Registry.ECR.SecretKey,
				}, nil
			}),
		)
	}
	ecrClient := ecr.NewFromConfig(awsCfg)
	return registry.NewECRProvider(ecrClient), nil
}

func newGHCRProvider(_ context.Context, cfg *config.Config) (*registry.GHCRProvider, error) {
	if cfg.Registry.GHCR == nil {
		return nil, fmt.Errorf("GHCR registry type requires ghcr configuration")
	}
	ghCfg := registry.GHCRConfig{
		Username: cfg.Registry.GHCR.Username,
		Password: cfg.Registry.GHCR.Password,
	}
	return registry.NewGHCRProvider(&http.Client{Timeout: 30 * time.Second}, ghCfg), nil
}

// newRouter builds one provider per configured registry block and routes
// images by host. GHCR and Docker Hub providers always build (anonymous when
// unconfigured); ECR and GCR only when their blocks are present. Returns a
// nil router when a configured ECR/GCR provider fails to build.
func newRouter(ctx context.Context, cfg *config.Config) (*registry.Router, []string) {
	ghCfg := registry.GHCRConfig{}
	if cfg.Registry.GHCR != nil {
		ghCfg.Username = cfg.Registry.GHCR.Username
		ghCfg.Password = cfg.Registry.GHCR.Password
	}
	dhCfg := registry.DockerHubConfig{}
	if cfg.Registry.DockerHub != nil {
		dhCfg.Username = cfg.Registry.DockerHub.Username
		dhCfg.Password = cfg.Registry.DockerHub.Password
		dhCfg.ConfigPath = cfg.Registry.DockerHub.ConfigPath
	}
	var (
		ecrP *registry.ECRProvider
		gcrP *registry.GCRProvider
		names []string
	)
	if cfg.Registry.ECR != nil {
		var err error
		ecrP, err = newECRProvider(ctx, cfg)
		if err != nil {
			return nil, nil
		}
		names = append(names, "ecr")
	}
	if cfg.Registry.GCR != nil {
		var err error
		gcrP, err = newGCRProvider(ctx, cfg)
		if err != nil {
			return nil, nil
		}
		names = append(names, "gcr")
	}
	names = append(names, "ghcr", "dockerhub")
	return registry.NewRouter(
		registry.NewGHCRProvider(&http.Client{Timeout: 30 * time.Second}, ghCfg),
		registry.NewDockerHubProvider(&http.Client{Timeout: 30 * time.Second}, dhCfg),
		ecrP,
		gcrP,
	), names
}

func newGCRProvider(ctx context.Context, cfg *config.Config) (*registry.GCRProvider, error) {
	if cfg.Registry.GCR == nil {
		return nil, fmt.Errorf("GCR registry type requires gcr configuration")
	}
	gcrCfg := registry.GCRConfig{
		AuthMethod:         cfg.Registry.GCR.Auth.Method,
		ServiceAccountFile: cfg.Registry.GCR.Auth.ServiceAccountFile,
	}
	return registry.NewGCRProvider(ctx, gcrCfg, &http.Client{Timeout: 30 * time.Second})
}

func newNotifier(cfg *config.Config) notifier.Notifier {
	tmpl := cfg.Notifications.Templates
	notifiers := []notifier.Notifier{
		notifier.NewLogNotifierWithTemplate(slog.Default(), tmpl.Default),
	}

	if cfg.Notifications.SlackWebhookURL != "" {
		notifiers = append(notifiers, notifier.NewSlackNotifierWithTemplate(cfg.Notifications.SlackWebhookURL, &http.Client{}, tmpl.Slack))
		slog.Info("Slack notifications enabled")
	}

	if cfg.Notifications.DiscordWebhookURL != "" {
		notifiers = append(notifiers, notifier.NewDiscordNotifierWithTemplate(cfg.Notifications.DiscordWebhookURL, &http.Client{}, tmpl.Discord))
		slog.Info("Discord notifications enabled")
	}

	if cfg.Notifications.TelegramBotToken != "" && cfg.Notifications.TelegramChatID != "" {
		notifiers = append(notifiers, notifier.NewTelegramNotifierWithTemplate(cfg.Notifications.TelegramBotToken, cfg.Notifications.TelegramChatID, &http.Client{}, tmpl.Telegram))
		slog.Info("Telegram notifications enabled")
	}

	if cfg.Notifications.MSTeamsWebhookURL != "" {
		notifiers = append(notifiers, notifier.NewMSTeamsNotifierWithTemplate(cfg.Notifications.MSTeamsWebhookURL, &http.Client{}, tmpl.MSTeams))
		slog.Info("Microsoft Teams notifications enabled")
	}

	if cfg.Notifications.GoogleChatWebhookURL != "" {
		notifiers = append(notifiers, notifier.NewGoogleChatNotifierWithTemplate(cfg.Notifications.GoogleChatWebhookURL, &http.Client{}, tmpl.GoogleChat))
		slog.Info("Google Chat notifications enabled")
	}

	if cfg.Notifications.EmailHost != "" && cfg.Notifications.EmailPort != "" {
		notifiers = append(notifiers, notifier.NewEmailNotifierWithTemplate(
			cfg.Notifications.EmailHost,
			cfg.Notifications.EmailPort,
			cfg.Notifications.EmailUser,
			cfg.Notifications.EmailPassword,
			cfg.Notifications.EmailFrom,
			cfg.Notifications.EmailTo,
			"", // subject template falls back to default
			tmpl.Email,
		))
		slog.Info("Email notifications enabled")
	}

	return notifier.NewCompositeNotifier(notifiers...)
}
