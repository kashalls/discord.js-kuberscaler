# Discord Gateway Sharding Operator

A Kubernetes operator that manages Discord gateway shard scaling for Discord bots.

## Overview

This operator automates the management of Discord gateway shards by:

- Defining a **Custom Resource (CRD)** for Discord bots (`DiscordGateway`)
- Creating and managing a **StatefulSet** where each pod represents exactly one shard
- Fetching recommended shard counts from Discord's API
- Scaling shards automatically based on Discord's recommendations or fixed configuration

## Features

- **Automatic Shard Scaling**: Queries Discord's `/gateway/bot` endpoint for recommended shard count
- **Flexible Sharding Modes**:
  - `Recommended`: Uses Discord's recommended shard count with optional min/max constraints
  - `Fixed`: Uses a user-defined fixed shard count
- **StatefulSet Management**: Creates one pod per shard with proper environment variables
- **GitOps Compatible**: Designed to work with ArgoCD/Flux
- **Status Tracking**: Reports applied shards, recommended shards, and sync status

## Quick Start

### Prerequisites

- Kubernetes cluster (v1.28+)
- kubectl configured to access your cluster
- Discord bot token stored in a Kubernetes Secret

### Installation

1. Install the CRDs:

```bash
kubectl apply -f config/crd/bases/discord.nerdz.io_discordgateways.yaml
```

2. Deploy the operator:

```bash
kubectl apply -f config/manager/manager.yaml
kubectl apply -f config/rbac/
```

3. Create a Secret with your Discord bot token:

```bash
kubectl create secret generic discord-bot-token \
  --from-literal=token=YOUR_BOT_TOKEN_HERE
```

4. Create a DiscordGateway resource:

```yaml
apiVersion: discord.nerdz.io/v1alpha1
kind: DiscordGateway
metadata:
  name: my-discord-bot
spec:
  image: ghcr.io/example/discord-bot:latest
  tokenSecretRef:
    name: discord-bot-token
    key: token
  sharding:
    mode: Recommended
    minShards: 1
    maxShards: 10
  intents:
    privileged: false
  podTemplate:
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: 500m
        memory: 512Mi
```

### Bot Application Requirements

Your Discord bot application should read these environment variables:

- `DISCORD_TOKEN`: Bot token (automatically injected from Secret)
- `DISCORD_SHARD_ID`: Pod name in format `<gateway-name>-<ordinal>` (use downward API via `metadata.name`)
- `DISCORD_SHARD_COUNT`: Total number of shards
- `DISCORD_MAX_CONCURRENCY`: Maximum concurrent shard connections allowed
- `DISCORD_INTENTS`: Intent configuration (currently "0" or "privileged")

**Important**: The `DISCORD_SHARD_ID` environment variable contains the full pod name (e.g., `my-discord-bot-0`). Your bot application must parse the shard ID from this name.

Example for parsing shard ID from pod name:

```javascript
// Pod names are formatted as: <gateway-name>-<ordinal>
// The ordinal IS the shard ID
const podName = process.env.DISCORD_SHARD_ID || process.env.HOSTNAME;
const shardId = parseInt(podName.split('-').pop());
const shardCount = parseInt(process.env.DISCORD_SHARD_COUNT);

console.log(`Starting shard ${shardId} of ${shardCount}`);
```

```python
# Python example
import os

pod_name = os.environ.get('DISCORD_SHARD_ID') or os.environ.get('HOSTNAME')
shard_id = int(pod_name.split('-')[-1])
shard_count = int(os.environ['DISCORD_SHARD_COUNT'])

print(f"Starting shard {shard_id} of {shard_count}")
```

```go
// Go example
package main

import (
    "fmt"
    "os"
    "strconv"
    "strings"
)

func getShardID() (int, error) {
    podName := os.Getenv("DISCORD_SHARD_ID")
    if podName == "" {
        podName = os.Getenv("HOSTNAME")
    }
    parts := strings.Split(podName, "-")
    return strconv.Atoi(parts[len(parts)-1])
}
```

## Architecture

### Components

- **DiscordGateway CRD**: Defines the desired state of a Discord bot's gateway configuration
- **Controller**: Reconciles DiscordGateway resources
- **Discord API Client**: Fetches gateway information from Discord
- **StatefulSet Builder**: Templates StatefulSets for shard pods

### What the Operator Does

- ✅ Manages shard count based on Discord recommendations
- ✅ Creates/updates StatefulSets with proper shard configuration
- ✅ Injects environment variables for shard ID and count
- ✅ Reports status and conditions

### What the Operator Does NOT Do

- ❌ Run Discord client code or process gateway events
- ❌ Manage application logic or business code
- ❌ Perform cross-shard coordination
- ❌ Implement autoscaling based on CPU/memory

## API Reference

### DiscordGatewaySpec

| Field | Type | Description |
|-------|------|-------------|
| `image` | string | Container image for shard pods |
| `tokenSecretRef` | SecretReference | Reference to Secret containing bot token |
| `sharding` | ShardingConfig | Sharding configuration |
| `intents` | IntentsConfig | Discord intents configuration |
| `podTemplate` | PodTemplate | Pod template configuration |

### ShardingConfig

| Field | Type | Description |
|-------|------|-------------|
| `mode` | enum | `Recommended` or `Fixed` |
| `fixedShardCount` | int32 | Fixed shard count (when mode is Fixed) |
| `minShards` | int32 | Minimum shards (Recommended mode) |
| `maxShards` | int32 | Maximum shards (Recommended mode) |

### DiscordGatewayStatus

| Field | Type | Description |
|-------|------|-------------|
| `recommendedShards` | int32 | Shard count recommended by Discord |
| `appliedShards` | int32 | Current number of deployed shards |
| `maxConcurrency` | int32 | Max concurrent connections from Discord |
| `lastSyncTime` | time | Last sync with Discord API |
| `conditions` | []Condition | Resource conditions |

## Development

### Building

```bash
make build
```

### Running Tests

```bash
make test
```

### Running Locally

```bash
make install  # Install CRDs
make run      # Run controller locally
```

### Building Docker Image

```bash
make docker-build IMG=myregistry/discord-gateway-operator:tag
make docker-push IMG=myregistry/discord-gateway-operator:tag
```

## GitOps Integration

The operator is designed to work seamlessly with GitOps tools:

- Store `DiscordGateway` resources in Git
- Operator manages derived resources (StatefulSets)
- Status updates don't conflict with GitOps reconciliation
- Owner references ensure proper resource cleanup

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

Apache License 2.0 - See [LICENSE](LICENSE) for details.
