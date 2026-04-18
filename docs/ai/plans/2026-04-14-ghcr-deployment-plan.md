# Automated GHCR Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automate Docker image publication to GitHub Container Registry (GHCR) with versioned tagging.

**Architecture:** Use GitHub Actions with `docker/build-push-action`. Implement a tagging strategy for `:latest` (on master) and `:v*` (on git tags).

**Tech Stack:** GitHub Actions, Docker Buildx, GHCR.

---

### Task 1: Configure Permissions and Environment

**Files:**
- Modify: `.github/workflows/build.yml`

- [ ] **Step 1: Add package permissions to the workflow**

Add `permissions` block at the job level.

```yaml
permissions:
  contents: read
  packages: write
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/build.yml
git commit -m "ci: add package write permissions for GHCR"
```

### Task 2: Implement Metadata and Login Steps

**Files:**
- Modify: `.github/workflows/build.yml`

- [ ] **Step 1: Add Docker Metadata and Login actions**

Add steps to extract metadata and login to GHCR.

```yaml
      - name: Log in to the Container registry
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata (tags, labels) for Docker
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ghcr.io/${{ github.repository }}
          tags: |
            type=raw,value=latest,enable=${{ github.ref == 'refs/heads/master' }}
            type=semver,pattern={{version}}
            type=sha,format=short
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/build.yml
git commit -m "ci: add docker login and metadata extraction"
```

### Task 3: Implement Build and Push with Caching

**Files:**
- Modify: `.github/workflows/build.yml`

- [ ] **Step 1: Replace Build Docker Image step with Build-and-Push**

Update the Docker build step to push images and use GitHub Actions caching.

```yaml
      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Build and push Docker image
        uses: docker/build-push-action@v5
        with:
          context: .
          file: infra/Dockerfile
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/build.yml
git commit -m "ci: implement automated build and push to GHCR"
```

### Task 4: Verification

- [ ] **Step 1: Verify YAML syntax**

Run: `actionlint .github/workflows/build.yml` (if available) or check for basic formatting.

- [ ] **Step 2: Check final file state**

Read the file to ensure all steps are correctly integrated.
