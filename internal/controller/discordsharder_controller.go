/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	discordv1alpha1 "github.com/waifulabs/discord.js-kuberscaler/api/v1alpha1"
	"github.com/waifulabs/discord.js-kuberscaler/internal/discord"
	"github.com/waifulabs/discord.js-kuberscaler/internal/k8s"
)

const (
	// ConditionTypeReady represents the Ready condition
	ConditionTypeReady = "Ready"
	// ConditionTypeDegraded represents the Degraded condition
	ConditionTypeDegraded = "Degraded"
	// ConditionTypePendingReshard indicates a shard count change is waiting to be applied.
	ConditionTypePendingReshard = "PendingReshard"
	// ReasonReconciling indicates the resource is being reconciled
	ReasonReconciling = "Reconciling"
	// ReasonAvailable indicates the resource is available
	ReasonAvailable = "Available"
	// ReasonFailed indicates reconciliation failed
	ReasonFailed = "Failed"
	// ReasonDiscordAPIError indicates an error calling Discord API
	ReasonDiscordAPIError = "DiscordAPIError"
	// ReasonWaitingForAnnotation indicates ChangeStrategy=OnAnnotation is blocking a reshard
	ReasonWaitingForAnnotation = "WaitingForAnnotation"
	// ReasonBlueGreenInProgress indicates a blue-green rollout is underway
	ReasonBlueGreenInProgress = "BlueGreenInProgress"

	// allowReshardAnnotation is checked when ChangeStrategy=OnAnnotation.
	allowReshardAnnotation = "discord.ok8.sh/allow-reshard"

	// defaultSyncInterval is used when spec.syncInterval is not set.
	defaultSyncInterval = 12 * time.Hour
)

// DiscordSharderReconciler reconciles a DiscordSharder object
type DiscordSharderReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	DiscordClient discord.GatewayClient
}

// +kubebuilder:rbac:groups=discord.ok8.sh,resources=discordsharders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=discord.ok8.sh,resources=discordsharders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=discord.ok8.sh,resources=discordsharders/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DiscordSharderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the DiscordSharder instance
	gateway := &discordv1alpha1.DiscordSharder{}
	if err := r.Get(ctx, req.NamespacedName, gateway); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("DiscordSharder resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get DiscordSharder")
		return ctrl.Result{}, err
	}

	interval := defaultSyncInterval
	if gateway.Spec.SyncInterval != nil && gateway.Spec.SyncInterval.Duration > 0 {
		interval = gateway.Spec.SyncInterval.Duration
	}

	// Validate sharding constraints before doing any external calls.
	if gateway.Spec.Sharding.MinShards != nil && gateway.Spec.Sharding.MaxShards != nil {
		if *gateway.Spec.Sharding.MinShards > *gateway.Spec.Sharding.MaxShards {
			msg := fmt.Sprintf("invalid sharding config: minShards (%d) cannot exceed maxShards (%d)",
				*gateway.Spec.Sharding.MinShards, *gateway.Spec.Sharding.MaxShards)
			logger.Error(nil, msg)
			meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
				Type:    ConditionTypeDegraded,
				Status:  metav1.ConditionTrue,
				Reason:  ReasonFailed,
				Message: msg,
			})
			meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
				Type:    ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  ReasonFailed,
				Message: msg,
			})
			if statusErr := r.Status().Update(ctx, gateway); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			// Do not requeue automatically — the user must fix their spec.
			return ctrl.Result{}, nil
		}
	}

	// Fetch the bot token from Secret
	token, err := r.getToken(ctx, gateway)
	if err != nil {
		logger.Error(err, "Failed to get bot token from Secret")
		meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonFailed,
			Message: fmt.Sprintf("Failed to get bot token: %v", err),
		})
		if statusErr := r.Status().Update(ctx, gateway); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: interval}, err
	}

	// Fetch recommended shard count from Discord
	gatewayInfo, err := r.DiscordClient.GetGatewayBot(ctx, token)
	if err != nil {
		logger.Error(err, "Failed to get gateway info from Discord")
		meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeDegraded,
			Status:  metav1.ConditionTrue,
			Reason:  ReasonDiscordAPIError,
			Message: fmt.Sprintf("Failed to fetch gateway info: %v", err),
		})
		meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonDiscordAPIError,
			Message: fmt.Sprintf("Discord API error: %v", err),
		})
		if statusErr := r.Status().Update(ctx, gateway); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: interval}, err
	}

	// Update status with Discord info
	now := metav1.Now()
	gateway.Status.RecommendedShards = int32(gatewayInfo.Shards)
	gateway.Status.MaxConcurrency = int32(gatewayInfo.SessionStartLimit.MaxConcurrency)
	gateway.Status.LastSyncTime = &now

	// Calculate desired shard count (applies mode, step-rounding, and min/max).
	desiredShards := r.calculateDesiredShards(gateway, int32(gatewayInfo.Shards))

	// ChangeStrategy=OnAnnotation: hold the change until the user annotates.
	changeStrategy := gateway.Spec.Sharding.ChangeStrategy
	if changeStrategy == "" {
		changeStrategy = discordv1alpha1.ChangeStrategyImmediate
	}
	if changeStrategy == discordv1alpha1.ChangeStrategyOnAnnotation &&
		gateway.Status.AppliedShards != 0 &&
		desiredShards != gateway.Status.AppliedShards {

		annotations := gateway.GetAnnotations()
		if annotations == nil || annotations[allowReshardAnnotation] != "true" {
			// Park the desired count in status so users can see it.
			gateway.Status.PendingShards = desiredShards
			meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
				Type:   ConditionTypePendingReshard,
				Status: metav1.ConditionTrue,
				Reason: ReasonWaitingForAnnotation,
				Message: fmt.Sprintf(
					"Shard count change from %d to %d is pending. "+
						"Add annotation %s: \"true\" to apply.",
					gateway.Status.AppliedShards, desiredShards, allowReshardAnnotation),
			})
			if statusErr := r.Status().Update(ctx, gateway); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{RequeueAfter: interval}, nil
		}

		// Annotation is present — consume it immediately so it acts as a
		// one-shot gate rather than a permanent override.
		patch := client.MergeFrom(gateway.DeepCopy())
		delete(gateway.Annotations, allowReshardAnnotation)
		if patchErr := r.Patch(ctx, gateway, patch); patchErr != nil {
			logger.Error(patchErr, "Failed to remove allow-reshard annotation")
		}
	}

	// Clear any stale PendingReshard condition/count now that we are applying.
	if gateway.Status.PendingShards != 0 && desiredShards == gateway.Status.AppliedShards {
		gateway.Status.PendingShards = 0
	}
	meta.RemoveStatusCondition(&gateway.Status.Conditions, ConditionTypePendingReshard)

	// Ensure the headless Service exists before the StatefulSet — the
	// StatefulSet spec.serviceName must reference an existing Service.
	if err := r.reconcileService(ctx, gateway); err != nil {
		logger.Error(err, "Failed to reconcile headless Service")
		meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonFailed,
			Message: fmt.Sprintf("Failed to reconcile Service: %v", err),
		})
		if statusErr := r.Status().Update(ctx, gateway); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Reconcile StatefulSet using the configured update strategy.
	updateStrategy := gateway.Spec.Sharding.UpdateStrategy
	if updateStrategy == "" {
		updateStrategy = discordv1alpha1.UpdateStrategyRollingUpdate
	}

	var reconcileErr error
	if updateStrategy == discordv1alpha1.UpdateStrategyBlueGreen {
		reconcileErr = r.reconcileStatefulSetBlueGreen(ctx, gateway, desiredShards)
	} else {
		reconcileErr = r.reconcileStatefulSet(ctx, gateway, desiredShards)
	}

	if reconcileErr != nil {
		logger.Error(reconcileErr, "Failed to reconcile StatefulSet")
		meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonFailed,
			Message: fmt.Sprintf("Failed to reconcile StatefulSet: %v", reconcileErr),
		})
		if statusErr := r.Status().Update(ctx, gateway); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, reconcileErr
	}

	// Only mark AppliedShards when a blue-green transition is not in flight.
	// During blue-green, AppliedShards reflects the active fleet until promotion.
	if updateStrategy != discordv1alpha1.UpdateStrategyBlueGreen || gateway.Status.PendingShards == 0 {
		gateway.Status.AppliedShards = desiredShards
	}

	// Set ready condition — if a blue-green rollout is underway, reflect that.
	if gateway.Status.PendingShards != 0 {
		meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
			Type:   ConditionTypeReady,
			Status: metav1.ConditionTrue,
			Reason: ReasonBlueGreenInProgress,
			Message: fmt.Sprintf(
				"Blue-green rollout in progress: waiting for %d-shard fleet to become Ready.",
				gateway.Status.PendingShards),
		})
	} else {
		meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  ReasonAvailable,
			Message: "DiscordSharder is ready",
		})
	}
	meta.RemoveStatusCondition(&gateway.Status.Conditions, ConditionTypeDegraded)

	if err := r.Status().Update(ctx, gateway); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled DiscordSharder",
		"recommendedShards", gateway.Status.RecommendedShards,
		"appliedShards", gateway.Status.AppliedShards,
		"pendingShards", gateway.Status.PendingShards,
		"maxConcurrency", gateway.Status.MaxConcurrency)

	return ctrl.Result{RequeueAfter: interval}, nil
}

// getToken retrieves the Discord bot token from the referenced Secret.
func (r *DiscordSharderReconciler) getToken(ctx context.Context, gateway *discordv1alpha1.DiscordSharder) (string, error) {
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      gateway.Spec.TokenSecretRef.Name,
		Namespace: gateway.Namespace,
	}

	if err := r.Get(ctx, secretKey, secret); err != nil {
		return "", fmt.Errorf("failed to get secret: %w", err)
	}

	tokenBytes, ok := secret.Data[gateway.Spec.TokenSecretRef.Key]
	if !ok {
		return "", fmt.Errorf("key %s not found in secret", gateway.Spec.TokenSecretRef.Key)
	}

	return string(tokenBytes), nil
}

// calculateDesiredShards determines the desired shard count.
// If fixedShardCount is set it is returned directly. Otherwise Discord's
// recommendation is step-rounded and clamped by minShards / maxShards.
func (r *DiscordSharderReconciler) calculateDesiredShards(gateway *discordv1alpha1.DiscordSharder, recommendedShards int32) int32 {
	if gateway.Spec.Sharding.FixedShardCount != nil {
		return *gateway.Spec.Sharding.FixedShardCount
	}

	// Recommended path: start from Discord's suggestion.
	desired := recommendedShards

	// Apply step-size rounding before min/max so that constraints are evaluated
	// against the already-rounded value.
	if gateway.Spec.Sharding.StepSize != nil && *gateway.Spec.Sharding.StepSize > 1 {
		step := *gateway.Spec.Sharding.StepSize
		desired = ((desired + step - 1) / step) * step
	}

	if gateway.Spec.Sharding.MinShards != nil && desired < *gateway.Spec.Sharding.MinShards {
		desired = *gateway.Spec.Sharding.MinShards
	}

	if gateway.Spec.Sharding.MaxShards != nil && desired > *gateway.Spec.Sharding.MaxShards {
		desired = *gateway.Spec.Sharding.MaxShards
	}

	return desired
}

// reconcileService ensures the headless Service owned by this DiscordSharder exists.
// The Service is only created, never updated — its spec.clusterIP is immutable and
// there are no other mutable fields of interest. If the Service is deleted, the
// owner-reference watch will trigger a reconcile that recreates it.
func (r *DiscordSharderReconciler) reconcileService(ctx context.Context, gateway *discordv1alpha1.DiscordSharder) error {
	logger := log.FromContext(ctx)

	desired := k8s.BuildHeadlessService(gateway)
	if err := controllerutil.SetControllerReference(gateway, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on Service: %w", err)
	}

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating headless Service", "name", desired.Name)
		if createErr := r.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("failed to create Service: %w", createErr)
		}
		return nil
	}
	return err
}

// reconcileStatefulSet creates or updates the StatefulSet for the gateway
// using the default RollingUpdate strategy.
func (r *DiscordSharderReconciler) reconcileStatefulSet(ctx context.Context, gateway *discordv1alpha1.DiscordSharder, replicas int32) error {
	return r.ensureStatefulSet(ctx, gateway, gateway.Name, replicas)
}

// reconcileStatefulSetBlueGreen manages a zero-disruption shard count change by
// running the new fleet alongside the old one until the new fleet is fully Ready,
// then removing the old fleet.
//
// State machine (tracked via gateway.Status.PendingShards and
// gateway.Status.ActiveStatefulSet):
//
//  1. No change needed → sync the active StatefulSet in place (no-op if count matches).
//  2. Change needed, no transition in flight → create the pending StatefulSet.
//  3. Transition in flight → poll pending readiness; promote when Ready.
func (r *DiscordSharderReconciler) reconcileStatefulSetBlueGreen(ctx context.Context, gateway *discordv1alpha1.DiscordSharder, desiredShards int32) error {
	logger := log.FromContext(ctx)

	activeName := activeSTSName(gateway)
	pendingName := pendingSTSName(gateway)

	// Case 1: no change needed — keep the active StatefulSet in sync.
	if gateway.Status.PendingShards == 0 && gateway.Status.AppliedShards == desiredShards {
		return r.ensureStatefulSet(ctx, gateway, activeName, desiredShards)
	}

	// Case 3: a transition is already in flight — check if the pending fleet is ready.
	if gateway.Status.PendingShards != 0 {
		pending := gateway.Status.PendingShards

		pendingSts := &appsv1.StatefulSet{}
		err := r.Get(ctx, types.NamespacedName{Name: pendingName, Namespace: gateway.Namespace}, pendingSts)
		if err != nil && apierrors.IsNotFound(err) {
			// Pending StatefulSet was removed externally; reset and re-evaluate.
			logger.Info("Pending StatefulSet not found, resetting blue-green state", "name", pendingName)
			gateway.Status.PendingShards = 0
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to get pending StatefulSet: %w", err)
		}

		if pendingSts.Status.ReadyReplicas == pending && pendingSts.Status.Replicas == pending {
			logger.Info("Pending StatefulSet is ready, promoting to active",
				"pendingName", pendingName, "shards", pending)

			// Delete the old active fleet.
			activeSts := &appsv1.StatefulSet{}
			getErr := r.Get(ctx, types.NamespacedName{Name: activeName, Namespace: gateway.Namespace}, activeSts)
			if getErr == nil {
				if delErr := r.Delete(ctx, activeSts); delErr != nil && !apierrors.IsNotFound(delErr) {
					return fmt.Errorf("failed to delete old StatefulSet %q: %w", activeName, delErr)
				}
			}

			// Promote: increment the revision so activeSTSName now returns
			// the promoted StatefulSet's name.
			gateway.Status.ActiveRevision++
			gateway.Status.AppliedShards = pending
			gateway.Status.PendingShards = 0
			return nil
		}

		logger.Info("Waiting for pending StatefulSet to become Ready",
			"name", pendingName,
			"readyReplicas", pendingSts.Status.ReadyReplicas,
			"desired", pending)
		return nil
	}

	// Case 2: change needed, no transition in flight — start one.
	logger.Info("Starting blue-green transition",
		"activeName", activeName,
		"pendingName", pendingName,
		"desiredShards", desiredShards)

	if err := r.ensureStatefulSet(ctx, gateway, pendingName, desiredShards); err != nil {
		return err
	}
	gateway.Status.PendingShards = desiredShards
	return nil
}

// ensureStatefulSet creates or updates a StatefulSet with the given name and
// replica count. It is the shared implementation for both RollingUpdate and
// BlueGreen reconcile paths.
func (r *DiscordSharderReconciler) ensureStatefulSet(ctx context.Context, gateway *discordv1alpha1.DiscordSharder, name string, replicas int32) error {
	logger := log.FromContext(ctx)

	desiredSts := k8s.BuildStatefulSet(gateway, name, replicas)

	if err := controllerutil.SetControllerReference(gateway, desiredSts, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	existingSts := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: gateway.Namespace}, existingSts)

	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating StatefulSet", "name", name, "replicas", replicas)
		if createErr := r.Create(ctx, desiredSts); createErr != nil {
			return fmt.Errorf("failed to create StatefulSet: %w", createErr)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Sync all mutable fields if the replica count has drifted.
	if existingSts.Spec.Replicas == nil || *existingSts.Spec.Replicas != replicas {
		logger.Info("Updating StatefulSet", "name", name, "replicas", replicas)
		existingSts.Labels = desiredSts.Labels
		existingSts.Spec.Replicas = desiredSts.Spec.Replicas
		existingSts.Spec.Template = desiredSts.Spec.Template
		existingSts.Spec.UpdateStrategy = desiredSts.Spec.UpdateStrategy

		if updateErr := r.Update(ctx, existingSts); updateErr != nil {
			return fmt.Errorf("failed to update StatefulSet: %w", updateErr)
		}
	}

	return nil
}

// activeSTSName returns the name of the currently active StatefulSet.
// Revision 0 returns the bare gateway name (backward-compatible with
// StatefulSets created before blue-green support was added).
func activeSTSName(gateway *discordv1alpha1.DiscordSharder) string {
	if gateway.Status.ActiveRevision == 0 {
		return gateway.Name
	}
	return fmt.Sprintf("%s-%d", gateway.Name, gateway.Status.ActiveRevision)
}

// pendingSTSName returns the name for the next (incoming) StatefulSet during a
// blue-green transition. It is always ActiveRevision+1, so successive names are
// <name>, <name>-1, <name>-2, … and at most two StatefulSets exist at once.
func pendingSTSName(gateway *discordv1alpha1.DiscordSharder) string {
	return fmt.Sprintf("%s-%d", gateway.Name, gateway.Status.ActiveRevision+1)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DiscordSharderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&discordv1alpha1.DiscordSharder{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Named("discordsharder").
		Complete(r)
}
