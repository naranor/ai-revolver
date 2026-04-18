# Design Spec: Automated Docker Deployment to GHCR

This document outlines the design for automating Docker image publication to the GitHub Container Registry (GHCR) for the AI Revolver project.

## 1. Overview
The goal is to provide a reliable, versioned, and secure way for users to consume AI Revolver via Docker images. By leveraging GitHub Actions and GHCR, we ensure tight integration with the source code and zero-cost infrastructure for public releases.

## 2. Infrastructure
- **Registry:** `ghcr.io` (GitHub Container Registry)
- **Image Name:** `ghcr.io/${{ github.repository_owner }}/ai-revolver`
- **Build Context:** Project Root (`.`)
- **Dockerfile:** `infra/Dockerfile`

## 3. Workflow Logic

### 3.1 Triggers
- **Push to master/main:** Updates the `latest` stable image.
- **Git Tags (v*):** Creates a "frozen" versioned release (e.g., `v1.0.0`).
- **Pull Requests:** (Optional) Build check only, no push to registry.

### 3.2 Tagging Strategy
Using `docker/metadata-action`, we will generate the following tags:
- `type=raw,value=latest,enable=${{ github.ref == 'refs/heads/master' }}`
- `type=semver,pattern={{version}}` (from git tags)
- `type=sha,format=short` (e.g., `sha-ad1234`)

## 4. Security & Permissions
The workflow will use the built-in `GITHUB_TOKEN`. To push to GHCR, the following permissions are required in the workflow file:
```yaml
permissions:
  contents: read
  packages: write
```

## 5. Build Optimization
To ensure fast and reliable builds, we will use **Docker Buildx**:
- **Caching:** Use `type=gha` (GitHub Actions cache) for both import and export.
- **Platforms:** Standard `linux/amd64` (can be expanded to `linux/arm64` if requested).

## 6. Metadata (OCI Labels)
Images will include standard labels:
- `org.opencontainers.image.source`: Link to the GitHub repo.
- `org.opencontainers.image.description`: Description of the AI Proxy.
- `org.opencontainers.image.licenses`: MIT.

## 7. Integration Steps
1. Update `.github/workflows/build.yml` to include the `publish` job.
2. Configure `docker/login-action` for `ghcr.io`.
3. Configure `docker/metadata-action` for tagging.
4. Configure `docker/build-push-action` for the final build and push.
