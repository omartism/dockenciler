# Dockenciler Implementation Progress

## Goal
Implement Dockenciler, a Golang-based Docker reconciler that automatically updates containers based on image criteria.

## Project Overview
- **Core Function**: Monitor Docker containers and update them when new images match criteria.
- **Registry Support**: AWS ECR (IAM keys, IMDSv2).
- **Configuration**: JSON or Environment Variables.
- **Update Strategies**: In-place, Rolling (Docker Swarm).
- **Targeting**: All containers or those with a specific label (default: `dockenciler.autoupdate=true`).
- **Notifications**: Email, Slack, MS Teams, Google Chat, Telegram, Discord (customizable templates).
- **Deployment**: Dockerized, multi-stage build, GitHub Actions for release.

## Revised Implementation Roadmap (Vertical Slices)
- [x] **Slice 0: Interface Design Spike**
    - Define core interfaces (`Registry`, `DockerClient`, `Notifier`, `Reconciler`).
    - Establish `testutil/` with mocks/fakes.
    - Write failing acceptance tests for the core flow.
- [x] **Slice 1: Core Reconciliation (Tracer Bullet)**
    - Basic loop: JSON config -> List containers -> Check ECR (IAM) -> In-place update.
    - Integrate `slog` for observability.
    - Design env override mapping in config struct.
    - Add Swarm `Service` type stubs to `DockerClient`.
    - Implement "capture full container spec" for safe recreation.
- [x] **Slice 2: Advanced Targeting & Image Matching**
    - Label filtering, Version/Regex matching, Image digests.
    - Table-driven tests for matching logic.
- [x] **Slice 3: Secure AWS Infrastructure Auth**
    - IMDSv2 Instance Role integration.
    - ECR token caching implementation.
- [x] **Slice 4: Orchestration & Update Strategies**
    - Docker Swarm support, Rolling updates (`ServiceUpdate`).
- [x] **Slice 4.5: Safety Rails**
    - Dry-run mode.
    - Self-update exclusion.
    - Dependency ordering.
    - State file for restart safety.
    - Old image pruning.
    - Audit log.
- [x] **Slice 5a: Notifications (Core)**
    - `Notifier` interface + Slack + Discord + Template engine.
    - Non-blocking dispatch (worker goroutine + buffered channel).
- [x] **Slice 5b: Notifications (Extended)**
    - Telegram, Email, MS Teams, Google Chat providers.
- [x] **Slice 6: Operational Hardening**
    - Implement env override logic.
    - Log verbosity knobs.
- [x] **Slice 7: Documentation & Examples**
    - README, example configs (ECR, Swarm, IMDSv2, Compose).
- [x] **Slice 8: Packaging & Release**
    - Multi-stage Dockerfile hardening.
    - GitHub Action for release with provenance/SBOM.

## Research & Context
- Docker SDK for Go: Used for container listing and updates.
- AWS SDK for Go v2: Used for ECR image checks.
- TDD Approach: Tests first for every slice.
- Oracle Feedback: Emphasized self-update exclusion, spec roundtripping, and interface stability.

## Plan & Reviews
- Plan Draft: Revised based on Oracle review.
- Oracle Review: Completed (m0004).

## Phase Status
- Phase 0: Pending
- Phase 1: Pending
- Phase 2: Pending
- Phase 3: Pending
- Phase 4: Pending
- Phase 4.5: Pending
- Phase 5a: Pending
- Phase 5b: Pending
- Phase 6: Pending
- Phase 7: Pending
- Phase 8: Pending

