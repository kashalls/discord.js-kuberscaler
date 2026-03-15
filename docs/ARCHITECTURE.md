# Architecture

This document describes the architecture and design decisions of the Discord Gateway Sharding Operator.

## Overview

The Discord Gateway Sharding Operator is a Kubernetes controller that automates the management of Discord bot shards. It follows the Kubernetes operator pattern using controller-runtime/kubebuilder.

## Components

### 1. Custom Resource Definition (CRD)

**File**: `api/v1alpha1/discordgateway_types.go`

The `DiscordGateway` CRD defines:
- **Spec**: User-provided configuration (image, sharding mode, resources, etc.)
- **Status**: Operator-managed state (recommended shards, applied shards, conditions)

Key design decisions:
- Two sharding modes: `Recommended` (uses Discord API) and `Fixed` (user-defined)
- Min/max constraints for recommended mode
- Status subresource for GitOps compatibility
- Printer columns for `kubectl get` output

### 2. Controller

**File**: `internal/controller/discordgateway_controller.go`

The reconciliation loop:

1. **Fetch DiscordGateway resource**
2. **Get bot token** from referenced Secret
3. **Query Discord API** for recommended shard count
4. **Calculate desired shards** based on mode and constraints
5. **Reconcile StatefulSet** (create or update)
6. **Update status** with current state

Reconciliation frequency: 10 minutes (configurable via `requeueInterval`)

Error handling:
- Discord API failures → Degraded condition, requeue
- Invalid Secret → Failed condition, requeue
- Rate limiting → Respects Retry-After header

### 3. Discord API Client

**File**: `internal/discord/gateway.go`

HTTP client for Discord's `/gateway/bot` endpoint:
- Returns: recommended shard count, max_concurrency, session limits
- Authentication: Bot token in Authorization header
- Error handling: Rate limiting, status codes, JSON parsing

Security:
- Token never logged or exposed in errors
- Used only for API authentication
- Read from Secret, kept in memory only during reconciliation

### 4. StatefulSet Builder

**File**: `internal/k8s/statefulset.go`

Creates StatefulSets with:
- **One pod per shard** (replica count = shard count)
- **Stable identities** (`<name>-0`, `<name>-1`, etc.)
- **Environment variables**:
  - `SHARDS`: Pod ordinal (via `apps.kubernetes.io/pod-index` label, K8s 1.28+); read natively by discord.js v14
  - `SHARD_COUNT`: Total shard count; read natively by discord.js v14
  - `DISCORD_TOKEN`: From Secret

Pod management:
- Parallel pod creation
- Rolling updates with max unavailable = 1
- Owner references for garbage collection

## Data Flow

```
┌─────────────────┐
│ DiscordGateway  │ (User creates CR)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   Controller    │
│  Reconciliation │
└────────┬────────┘
         │
         ├─────────────────┐
         │                 │
         ▼                 ▼
┌──────────────┐  ┌───────────────┐
│ Discord API  │  │    Secret     │
│  (Gateway)   │  │  (Bot Token)  │
└──────┬───────┘  └───────┬───────┘
       │                  │
       │                  │
       └──────┬───────────┘
              │
              ▼
      ┌───────────────┐
      │ Calculate     │
      │ Desired State │
      └───────┬───────┘
              │
              ▼
      ┌───────────────┐
      │ StatefulSet   │
      │  Reconcile    │
      └───────┬───────┘
              │
              ▼
      ┌───────────────┐
      │  Update CR    │
      │    Status     │
      └───────────────┘
```

## Shard Calculation Logic

### Recommended Mode

```go
desired = discord.recommendedShards

if minShards is set and desired < minShards:
    desired = minShards

if maxShards is set and desired > maxShards:
    desired = maxShards

return desired
```

### Fixed Mode

```go
return fixedShardCount (or 1 if not set)
```

## StatefulSet Design

### Why StatefulSet?

- **Stable network identities**: Each shard pod has a predictable name
- **Ordered deployment**: Pods created sequentially (though we use Parallel)
- **Persistent storage support**: If needed for bot data
- **Graceful scaling**: Kubernetes handles pod lifecycle

### Shard ID Assignment

Shard IDs are derived from pod ordinals:
- Pod `my-bot-0` → Shard ID 0
- Pod `my-bot-1` → Shard ID 1
- Pod `my-bot-N` → Shard ID N

The pod ordinal is projected into the `SHARDS` environment variable via the downward API using the `apps.kubernetes.io/pod-index` label (automatically set by Kubernetes 1.28+ on StatefulSet pods). discord.js v14 reads `SHARDS` and `SHARD_COUNT` natively without any application-side parsing.

## RBAC Permissions

The operator requires:

- **DiscordGateway**: Full CRUD + status updates
- **StatefulSet**: Full CRUD (for managing shard pods)
- **Secret**: Read-only (for bot token)

No cluster-admin required.

## GitOps Compatibility

Design features for GitOps:

1. **Owner references**: StatefulSets owned by DiscordGateway
2. **Status subresource**: Status updates don't trigger reconciliation
3. **Declarative spec**: All config in CR, no imperative operations
4. **Idempotent reconciliation**: Same input always produces same output

Works with:
- ArgoCD
- Flux
- Any Git-based deployment tool

## Scaling Behavior

### Scale Up

1. Operator detects increased recommended shard count
2. Updates StatefulSet replica count
3. Kubernetes creates new pods
4. New shards connect to Discord

### Scale Down

1. Operator detects decreased recommended shard count
2. Updates StatefulSet replica count
3. Kubernetes terminates highest-ordinal pods first
4. Remaining shards continue operating

### Scaling Constraints

- Minimum interval: 10 minutes (reconciliation period)
- Respects Discord's rate limits
- Honors min/max shard constraints

## Error Handling & Resilience

### Discord API Failures

- Set `Degraded` condition
- Preserve existing shard count (don't scale)
- Retry after interval

### Secret Not Found

- Set `Failed` condition
- Don't create StatefulSet
- Retry after interval

### Rate Limiting

- Parse `Retry-After` header
- Increase backoff
- Report in condition message

### Network Issues

- Transient errors trigger requeue
- Permanent errors reported in status
- Existing shards unaffected

## Testing Strategy

### Unit Tests

- Shard calculation logic (`internal/controller/sharding_test.go`)
- Pure functions, no Kubernetes dependency
- Fast execution, high coverage

### Integration Tests

- Controller behavior with fake Kubernetes client
- Mock Discord API client
- Verify StatefulSet creation/updates

### End-to-End Tests

- Not included (requires real cluster and Discord token)
- Can be added using envtest or kind

## Security Considerations

1. **Token Handling**:
   - Never logged or exposed
   - Read from Secret only during reconciliation
   - Not stored in operator state

2. **RBAC**:
   - Minimal permissions
   - No write access to Secrets
   - Namespace-scoped (can be cluster-scoped)

3. **API Communication**:
   - HTTPS only (Discord API)
   - Timeout configured (10 seconds)
   - No sensitive data in error messages

## Performance Characteristics

- **Reconciliation overhead**: Minimal (one API call per 10 minutes)
- **Memory usage**: O(1) per DiscordGateway resource
- **API calls**: 1 per reconciliation per resource
- **Kubernetes API calls**: O(1) per reconciliation

Recommended limits:
- ~100 DiscordGateway resources per operator instance
- Scales horizontally with leader election

## Future Enhancements

Possible improvements:

1. **Custom sync intervals** per DiscordGateway
2. **Shard affinity** (pin shards to nodes)
3. **Custom intents calculation** (bitmask generation)
4. **Metrics export** (Prometheus)
5. **Webhooks** for validation/defaulting
6. **Multi-cluster support** (shard distribution)
7. **Graceful shard migration** (zero-downtime rescaling)

## Design Philosophy

### What the Operator Does

- Manages the **quantity** of shard pods
- Configures **environment variables** for sharding
- Queries Discord for **recommendations**
- Reports **status** to users

### What the Operator Does NOT Do

- Run Discord gateway clients (that's the bot's job)
- Process Discord events
- Manage bot application logic
- Implement custom autoscaling metrics
- Handle shard coordination or state

The operator is a **control plane component** only. The actual Discord connection and event processing is the responsibility of the bot application running in the pods.
