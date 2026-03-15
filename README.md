# Discord Sharder Operator

A Kubernetes operator that runs one Discord gateway shard per pod — no multi-process sharding manager required.

## How It Works

1. You create a `DiscordSharder` custom resource with your pod template.
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
kubectl apply -f config/crd/bases/discord.ok8.sh_discordsharders.yaml
kubectl apply -f config/rbac/
kubectl apply -f config/manager/manager.yaml
```

### 2. Store your bot token in a Secret

```bash
kubectl create secret generic discord-bot-token \
  --from-literal=token=YOUR_BOT_TOKEN_HERE
```

Or use an [ExternalSecret](https://external-secrets.io/) to sync it from Vault, AWS Secrets Manager, etc. — just make sure the resulting Secret name and key match what you put in `spec.tokenSecretRef` and your container's `secretKeyRef`.

### 3. Create a DiscordSharder resource

```yaml
apiVersion: discord.ok8.sh/v1alpha1
kind: DiscordSharder
metadata:
  name: my-bot
spec:
  tokenSecretRef:
    name: discord-bot-token  # used by the operator to call Discord's API
    key: token

  sharding:
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
kubectl apply -f discordsharder.yaml
kubectl get discordsharder my-bot
```

### 4. Write your bot to read the injected variables

The operator injects `SHARDS` (the pod ordinal) and `SHARD_COUNT`. discord.js v14 reads these natively — no parsing required:

**JavaScript / TypeScript (discord.js v14)**

```js
const { Client, GatewayIntentBits } = require('discord.js');

// discord.js reads SHARDS and SHARD_COUNT automatically from the environment.
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

## Sharding Configuration

### Recommended (default)

Omit `fixedShardCount` and the operator uses Discord's recommended shard count, polled every 12 hours (configurable), clamped to your bounds:

```yaml
sharding:
  minShards: 2    # never fewer than 2
  maxShards: 50   # never more than 50
```

### Fixed

Set `fixedShardCount` to pin the shard count and ignore Discord's recommendation entirely:

```yaml
sharding:
  fixedShardCount: 16
```

### Step-size rounding

`stepSize` rounds Discord's recommendation up to the next multiple before applying min/max. This reduces how often a shard count change triggers a full restart as your bot grows gradually:

```yaml
sharding:
  stepSize: 4       # keeps counts at 4, 8, 12, 16, …
  maxShards: 64
```

With `stepSize: 4`, Discord recommending 5 shards results in 8 deployed — and the next restart won't happen until the recommendation exceeds 8 (triggering a jump to 12).

Powers of two (`stepSize: 2`, `4`, `8`, …) are especially efficient because `(guild_id >> 22) % num_shards` splits guild domains cleanly on doubling.

## Rollout Strategies

### RollingUpdate (default)

The existing StatefulSet is updated in-place. Kubernetes restarts pods one at a time as `SHARD_COUNT` changes.

```yaml
sharding:
  updateStrategy: RollingUpdate
```

### BlueGreen

A second StatefulSet is created with the new shard count. Once every pod in the incoming fleet is Ready, the old fleet is deleted. Both fleets run concurrently during the transition; deduplicate events in your application if that is a concern.

```yaml
sharding:
  updateStrategy: BlueGreen
```

The incoming StatefulSet is named `<name>-<revision>` (e.g. `my-bot-1`, `my-bot-2`, …). The active revision is visible in `status.activeRevision`. At most two StatefulSets exist at any time.

## Change Control

### Immediate (default)

Shard count changes are applied as soon as they are detected.

### OnAnnotation

Shard count changes are held until you explicitly approve them. The operator records the pending count in `status.pendingShards` and sets a `PendingReshard` condition.

```yaml
sharding:
  changeStrategy: OnAnnotation
```

To apply a pending change:

```bash
kubectl annotate discordsharder my-bot discord.ok8.sh/allow-reshard=true
```

The annotation is removed automatically once the change has been initiated (one-shot gate). Combine with `updateStrategy: BlueGreen` for full control: the new fleet only starts when you say so, and you can watch it become Ready before the old fleet is removed.

## Sync Interval

Controls how often the operator polls Discord's `/gateway/bot` API. Default is 12 hours.

```yaml
spec:
  syncInterval: 6h    # check every 6 hours instead
```

Valid Go duration strings: `30m`, `1h`, `12h`, `24h`, etc.

## Checking Status

```bash
kubectl get discordsharder
# NAME     APPLIED SHARDS   RECOMMENDED   READY   AGE
# my-bot   4                4             True    2m

kubectl describe discordsharder my-bot
```

Status fields:

| Field | Description |
|---|---|
| `appliedShards` | Number of replicas currently running |
| `recommendedShards` | What Discord's API returned last sync |
| `pendingShards` | Desired count waiting to be applied (OnAnnotation or BlueGreen in progress) |
| `activeRevision` | Current StatefulSet revision (BlueGreen only) |
| `maxConcurrency` | Discord's session-start concurrency limit |
| `lastSyncTime` | When the operator last called Discord's API |
| `conditions` | `Ready`, `Degraded`, `PendingReshard` conditions |

## Using ExternalSecrets

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

Then your `DiscordSharder` references it the same way as a regular Secret via `spec.tokenSecretRef`.

## API Reference

### DiscordSharderSpec

| Field | Type | Required | Description |
|---|---|---|---|
| `tokenSecretRef` | SecretReference | Yes | Secret containing the bot token, used by the operator to call Discord's API |
| `syncInterval` | Duration | No | How often to poll Discord's API (default: `12h`) |
| `sharding` | ShardingConfig | No | Shard count and rollout configuration |
| `template` | PodTemplateSpec | Yes | Standard Kubernetes pod template for shard pods |

### ShardingConfig

| Field | Type | Default | Description |
|---|---|---|---|
| `fixedShardCount` | int32 | — | Pin to an exact shard count; omit to use Discord's recommendation |
| `minShards` | int32 | — | Lower bound on recommended shard count |
| `maxShards` | int32 | — | Upper bound on recommended shard count |
| `stepSize` | int32 | — | Round recommended count up to next multiple (reduces restart frequency) |
| `updateStrategy` | `RollingUpdate` \| `BlueGreen` | `RollingUpdate` | How shard count changes are rolled out |
| `changeStrategy` | `Immediate` \| `OnAnnotation` | `Immediate` | When shard count changes are applied |

### SecretReference

| Field | Type | Description |
|---|---|---|
| `name` | string | Name of the Kubernetes Secret |
| `key` | string | Key within the Secret |

## Development

```bash
make build          # compile
make test           # unit + integration tests (downloads envtest binaries automatically)
make lint           # lint
make install        # install CRDs into current cluster
make run            # run controller locally against current cluster
make docker-build IMG=myregistry/discord-sharder-operator:tag
make docker-push  IMG=myregistry/discord-sharder-operator:tag
```

## GitOps

The operator is designed for GitOps (ArgoCD, Flux):

- Store your `DiscordSharder` manifests in Git.
- The operator manages derived resources (StatefulSet, headless Service) via owner references.
- Status updates use the `/status` subresource and don't conflict with GitOps reconciliation.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
