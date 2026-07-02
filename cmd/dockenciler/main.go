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
	}

	interval, err := time.ParseDuration(cfg.ReconcileInterval)
	if err != nil {
		slog.Error("Invalid reconcile interval", "interval", cfg.ReconcileInterval, "error", err)
		os.Exit(1)
	}

	slog.Info("Starting dockenciler", "interval", interval, "label_filter", cfg.Docker.LabelFilter)

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
  ____                           _                _       
 |  _ \ _ __ ___  ___  ___  _ __| |___      _____| | ___  
 | | | | '__/ _ \/ __|/ _ \| '__| __\ \ /\ / / _ \ |/ _ \ 
 | |_| | | |  __/\__ \ (_) | |  | |_ \ V  V /  __/ | (_) |
 |____/|_|  \___||___/\___/|_|   \__| \_/\_/ \___|_|\___/ 
`
	slog.Info(banner)
	slog.Info("Dockenciler started", "version", "alpha")
}

func newECRProvider(ctx context.Context, cfg *config.Config) (*registry.ECRProvider, error) {
	awsCfg, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion(cfg.Registry.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	if cfg.Registry.AccessKey != "" && cfg.Registry.SecretKey != "" {
		awsCfg.Credentials = aws.NewCredentialsCache(
			aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
				return aws.Credentials{
					AccessKeyID:     cfg.Registry.AccessKey,
					SecretAccessKey: cfg.Registry.SecretKey,
				}, nil
			}),
		)
	}

	ecrClient := ecr.NewFromConfig(awsCfg)
	return registry.NewECRProvider(ecrClient), nil
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
