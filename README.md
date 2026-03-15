# Discord Gateway Sharding Operator

A Kubernetes operator that runs one Discord gateway shard per pod — no multi-process sharding manager required.

## How It Works

1. You create a `DiscordGateway` custom resource with your pod template.
2. The operator calls Discord's `/gateway/bot` API to get the recommended shard count.
3. The operator creates a StatefulSet scaled to that many replicas using your template.
4. Each pod is one shard. Pod `my-bot-0` handles shard 0, `my-bot-1` handles shard 1, and so on.

The operator injects two environment variables into every container (you can override either by declaring them yourself in your template):

| Variable | Value | Notes |
|---|---|---|
| `SHARDS` | Numeric pod ordinal, e.g. `3` | discord.js v14 reads this natively via `JSON.parse(process.env.SHARDS)` |
| `SHARD_COUNT` | Total number of shards | discord.js v14 reads this natively via `process.env.SHARD_COUNT` |

Requires Kubernetes 1.28+ (uses the `apps.kubernetes.io/pod-index` label added automatically to StatefulSet pods).

Everything else — your container image, `DISCORD_TOKEN`, resource limits, volumes, sidecars — goes in `spec.template`, which is a standard Kubernetes `PodTemplateSpec`.

## Quick Start

### 1. Install the CRD and operator

```bash
kubectl apply -f config/crd/bases/discord.ok8.sh_discordgateways.yaml
kubectl apply -f config/rbac/
kubectl apply -f config/manager/manager.yaml
```

### 2. Store your bot token in a Secret

```bash
kubectl create secret generic discord-bot-token \
  --from-literal=token=YOUR_BOT_TOKEN_HERE
```

Or use an [ExternalSecret](https://external-secrets.io/) to sync it from Vault, AWS Secrets Manager, etc. — just make sure the resulting Secret name and key match what you put in `spec.tokenSecretRef` and your container's `secretKeyRef`.

### 3. Create a DiscordGateway resource

```yaml
apiVersion: discord.ok8.sh/v1alpha1
kind: DiscordGateway
metadata:
  name: my-bot
spec:
  tokenSecretRef:
    name: discord-bot-token  # used by the operator to call Discord's API
    key: token

  sharding:
    mode: Recommended  # let Discord decide the shard count
    minShards: 1
    maxShards: 20

  template:
    spec:
      containers:
        - name: bot
          image: ghcr.io/my-org/my-discord-bot:latest
          env:
            - name: DISCORD_TOKEN
              valueFrom:
                secretKeyRef:
                  name: discord-bot-token
                  key: token
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
```

```bash
kubectl apply -f discordgateway.yaml
kubectl get discordgateway my-bot
```

### 4. Write your bot to read the injected variables

The operator injects `SHARDS` (the pod ordinal) and `SHARD_COUNT`. discord.js v14 reads these natively — no parsing required:

**JavaScript / TypeScript (discord.js v14)**

```js
const { Client, GatewayIntentBits } = require('discord.js');

// discord.js reads SHARDS and SHARD_COUNT automatically from the environment.
// No manual configuration needed — just create the client normally.
const client = new Client({
  intents: [GatewayIntentBits.Guilds],
});

client.login(process.env.DISCORD_TOKEN);
```

**Python (discord.py)**

```python
import os, discord

shard_id = int(os.environ['SHARDS'])
shard_count = int(os.environ['SHARD_COUNT'])

client = discord.AutoShardedClient(
    shard_id=shard_id,
    shard_count=shard_count,
)
client.run(os.environ['DISCORD_TOKEN'])
```

**Go**

```go
shardID, _ := strconv.Atoi(os.Getenv("SHARDS"))
shardCount, _ := strconv.Atoi(os.Getenv("SHARD_COUNT"))
```

## Using ExternalSecrets

If you manage secrets externally (Vault, AWS Secrets Manager, GCP Secret Manager, etc.) you can use the [External Secrets Operator](https://external-secrets.io/) to provision the Secret and reference it in `spec.tokenSecretRef` and your container env exactly the same way:

```yaml
# ExternalSecret creates a Secret named "discord-bot-token"
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: discord-bot-token
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend
    kind: ClusterSecretStore
  target:
    name: discord-bot-token
  data:
    - secretKey: token
      remoteRef:
        key: discord/my-bot
        property: token
```

Then your `DiscordGateway` references it the same way:

```yaml
spec:
  tokenSecretRef:
    name: discord-bot-token
    key: token
  template:
    spec:
      containers:
        - name: bot
          image: ghcr.io/my-org/my-discord-bot:latest
          env:
            - name: DISCORD_TOKEN
              valueFrom:
                secretKeyRef:
                  name: discord-bot-token
                  key: token
```

## Sharding Modes

### Recommended (default)

The operator calls Discord's API on every reconcile (every 10 minutes) and uses the shard count Discord recommends, clamped to your min/max bounds:

```yaml
sharding:
  mode: Recommended
  minShards: 2    # never fewer than 2
  maxShards: 50   # never more than 50
```

### Fixed

Use a specific shard count regardless of what Discord recommends. Useful for large bots with predictable guild counts or when you want deterministic deployments:

```yaml
sharding:
  mode: Fixed
  fixedShardCount: 16
```

## Checking Status

```bash
kubectl get discordgateway
# NAME     MODE          APPLIED SHARDS   RECOMMENDED   READY   AGE
# my-bot   Recommended   4                4             True    2m

kubectl describe discordgateway my-bot
```

Status fields:

| Field | Description |
|---|---|
| `appliedShards` | Number of replicas currently running |
| `recommendedShards` | What Discord's API returned |
| `maxConcurrency` | Discord's session-start concurrency limit |
| `lastSyncTime` | When the operator last called Discord's API |
| `conditions` | `Ready` and `Degraded` conditions |

## API Reference

### DiscordGatewaySpec

| Field | Type | Required | Description |
|---|---|---|---|
| `tokenSecretRef` | SecretReference | Yes | Secret containing the bot token, used by the operator to call Discord's API |
| `sharding` | ShardingConfig | No | Shard count configuration (defaults to Recommended) |
| `template` | PodTemplateSpec | Yes | Standard Kubernetes pod template for shard pods |

### ShardingConfig

| Field | Type | Description |
|---|---|---|
| `mode` | `Recommended` \| `Fixed` | How shard count is determined (default: `Recommended`) |
| `fixedShardCount` | int32 | Exact shard count when `mode: Fixed` |
| `minShards` | int32 | Lower bound when `mode: Recommended` |
| `maxShards` | int32 | Upper bound when `mode: Recommended` |

### SecretReference

| Field | Type | Description |
|---|---|---|
| `name` | string | Name of the Kubernetes Secret |
| `key` | string | Key within the Secret |

## Development

```bash
make build          # compile
make test           # unit + integration tests
make lint           # lint
make install        # install CRDs into current cluster
make run            # run controller locally against current cluster
make docker-build IMG=myregistry/discord-gateway-operator:tag
make docker-push  IMG=myregistry/discord-gateway-operator:tag
```

## GitOps

The operator is designed for GitOps (ArgoCD, Flux):

- Store your `DiscordGateway` manifests in Git.
- The operator manages derived resources (StatefulSet, headless Service) via owner references.
- Status updates use the `/status` subresource and don't conflict with GitOps reconciliation.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
