# Deployment Guide

This guide walks you through deploying the Discord Gateway Sharding Operator.

## Prerequisites

- Kubernetes cluster (v1.28+)
- kubectl configured
- Discord bot token
- Container registry access (for custom bot image)

## Step 1: Install CRDs

```bash
kubectl apply -f config/crd/bases/discord.ok8.sh_discordsharders.yaml
```

Verify the CRD is installed:

```bash
kubectl get crd discordsharders.discord.ok8.sh
```

## Step 2: Create Namespace (Optional)

```bash
kubectl create namespace discord-bots
```

## Step 3: Deploy the Operator

### Option A: Using kustomize (Recommended)

```bash
cd config/default
kustomize build . | kubectl apply -f -
```

### Option B: Manual Deployment

1. Create service account and RBAC:

```bash
kubectl apply -f config/rbac/
```

2. Deploy the operator:

```bash
kubectl apply -f config/manager/manager.yaml
```

Verify the operator is running:

```bash
kubectl get pods -n discord-kuberscaler-system
```

## Step 4: Create Discord Bot Token Secret

```bash
kubectl create secret generic discord-bot-token \
  --from-literal=token=YOUR_DISCORD_BOT_TOKEN \
  -n discord-bots
```

## Step 5: Deploy a DiscordSharder Resource

See `config/samples/discord_v1alpha1_discordsharder.yaml` for an example.

Apply it:

```bash
kubectl apply -f config/samples/discord_v1alpha1_discordsharder.yaml
```

## Step 6: Verify Deployment

Check the DiscordSharder status:

```bash
kubectl get discordsharder -o wide
```

Check the created StatefulSet and pods:

```bash
kubectl get statefulset
kubectl get pods
```

## Troubleshooting

See the main [README](../README.md) for detailed troubleshooting steps.
