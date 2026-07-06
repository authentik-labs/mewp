# cherry-pick-svc

Automatically creates backport pull requests when a `backport/<branch>` label is added to a merged PR.

## How it works

1. A PR is merged with one or more `backport/<target-branch>` labels, **or** a `backport/*` label is added to an already-merged PR.
2. The service cherry-picks the merge commit onto the target branch and pushes it as `cherry-pick/<pr-number>-to-<target-branch>`.
3. A new PR is opened, labelled `cherry-pick`, and assigned to the original author.
4. If the cherry-pick has conflicts, the conflicting files are committed as-is and the PR is flagged for manual resolution.
5. A comment is posted on the original PR with a link to the new one.

## GitHub App service

### 1. Create a GitHub App

Go to **Settings → Developer settings → GitHub Apps → New GitHub App** and configure:

| Setting | Value |
|---|---|
| Webhook URL | `https://<your-host>/webhook` |
| Webhook secret | A random string — store it as `WEBHOOK_SECRET` |

**Repository permissions:**

| Permission | Access |
|---|---|
| Contents | Read & Write |
| Pull requests | Read & Write |
| Issues | Read-only (required to subscribe to Issue events) |
| Metadata | Read (mandatory for all Apps) |

**Webhook events to subscribe to:**

| Event | Why |
|---|---|
| Pull request | Triggers on `closed` (merged) and `labeled` |
| Issues | Triggers on `labeled` for PRs from external forks — GitHub sends `issues.labeled` instead of `pull_request.labeled` in that case |

After creating the app, generate a private key and note the App ID — both are required at runtime.

### 2. Install the App

Install the GitHub App on the repository (or organisation) you want it to operate on.

### 3. Configuration

The service is configured entirely through environment variables.

| Variable | Required | Default | Description |
|---|---|---|---|
| `GITHUB_APP_ID` | yes | — | Numeric App ID shown on the App settings page |
| `GITHUB_APP_PRIVATE_KEY` | yes | — | PEM private key generated for the App; literal `\n` sequences are accepted (common when storing in env vars) |
| `WEBHOOK_SECRET` | yes | — | Secret set on the App's webhook configuration |
| `LISTEN_ADDR` | no | `:8080` | TCP address to bind |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, or `error` |

The service resolves its git commit identity automatically from the App's slug and bot user ID — no additional git configuration is needed.

### 4. Running

**Local:**

```bash
export GITHUB_APP_ID=...
export GITHUB_APP_PRIVATE_KEY="$(cat private-key.pem)"
export WEBHOOK_SECRET=...
go run ./cmd/server
```

**Docker:**

```bash
docker build -t cherry-pick-svc .
docker run -p 8080:8080 --env-file .env cherry-pick-svc
```

**Kubernetes:**

Create the secret first:

```bash
kubectl create secret generic cherry-pick-svc-secret \
  --from-literal=github-app-id=<app-id> \
  --from-file=github-app-private-key=private-key.pem \
  --from-literal=webhook-secret=<webhook-secret>
```

Then apply the manifests:

```bash
kubectl apply -f deploy/
```

**Health check:** `GET /healthz` returns `200 ok`.
