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
	// ReasonReconciling indicates the resource is being reconciled
	ReasonReconciling = "Reconciling"
	// ReasonAvailable indicates the resource is available
	ReasonAvailable = "Available"
	// ReasonFailed indicates reconciliation failed
	ReasonFailed = "Failed"
	// ReasonDiscordAPIError indicates an error calling Discord API
	ReasonDiscordAPIError = "DiscordAPIError"

	// Default requeue interval
	requeueInterval = 10 * time.Minute
)

// DiscordGatewayReconciler reconciles a DiscordGateway object
type DiscordGatewayReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	DiscordClient discord.GatewayClient
}

// +kubebuilder:rbac:groups=discord.ok8.sh,resources=discordgateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=discord.ok8.sh,resources=discordgateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=discord.ok8.sh,resources=discordgateways/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DiscordGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the DiscordGateway instance
	gateway := &discordv1alpha1.DiscordGateway{}
	if err := r.Get(ctx, req.NamespacedName, gateway); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("DiscordGateway resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get DiscordGateway")
		return ctrl.Result{}, err
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
		return ctrl.Result{RequeueAfter: requeueInterval}, err
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
		return ctrl.Result{RequeueAfter: requeueInterval}, err
	}

	// Update status with Discord info
	now := metav1.Now()
	gateway.Status.RecommendedShards = int32(gatewayInfo.Shards)
	gateway.Status.MaxConcurrency = int32(gatewayInfo.SessionStartLimit.MaxConcurrency)
	gateway.Status.LastSyncTime = &now

	// Calculate desired shard count
	desiredShards := r.calculateDesiredShards(gateway, int32(gatewayInfo.Shards))

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

	// Reconcile StatefulSet
	if err := r.reconcileStatefulSet(ctx, gateway, desiredShards); err != nil {
		logger.Error(err, "Failed to reconcile StatefulSet")
		meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonFailed,
			Message: fmt.Sprintf("Failed to reconcile StatefulSet: %v", err),
		})
		if statusErr := r.Status().Update(ctx, gateway); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	gateway.Status.AppliedShards = desiredShards

	// Set ready condition
	meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
		Type:    ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonAvailable,
		Message: "DiscordGateway is ready",
	})
	meta.RemoveStatusCondition(&gateway.Status.Conditions, ConditionTypeDegraded)

	if err := r.Status().Update(ctx, gateway); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled DiscordGateway",
		"recommendedShards", gateway.Status.RecommendedShards,
		"appliedShards", gateway.Status.AppliedShards,
		"maxConcurrency", gateway.Status.MaxConcurrency)

	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

// getToken retrieves the Discord bot token from the referenced Secret.
func (r *DiscordGatewayReconciler) getToken(ctx context.Context, gateway *discordv1alpha1.DiscordGateway) (string, error) {
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

// calculateDesiredShards determines the desired shard count based on the sharding mode.
func (r *DiscordGatewayReconciler) calculateDesiredShards(gateway *discordv1alpha1.DiscordGateway, recommendedShards int32) int32 {
	mode := gateway.Spec.Sharding.Mode
	if mode == "" {
		mode = discordv1alpha1.ShardingModeRecommended
	}

	if mode == discordv1alpha1.ShardingModeFixed {
		if gateway.Spec.Sharding.FixedShardCount != nil {
			return *gateway.Spec.Sharding.FixedShardCount
		}
		return 1
	}

	// Recommended mode
	desired := recommendedShards

	// Apply min constraint
	if gateway.Spec.Sharding.MinShards != nil && desired < *gateway.Spec.Sharding.MinShards {
		desired = *gateway.Spec.Sharding.MinShards
	}

	// Apply max constraint
	if gateway.Spec.Sharding.MaxShards != nil && desired > *gateway.Spec.Sharding.MaxShards {
		desired = *gateway.Spec.Sharding.MaxShards
	}

	return desired
}

// reconcileService ensures the headless Service owned by this DiscordGateway exists.
// The Service is only created, never updated — its spec.clusterIP is immutable and
// there are no other mutable fields of interest. If the Service is deleted, the
// owner-reference watch will trigger a reconcile that recreates it.
func (r *DiscordGatewayReconciler) reconcileService(ctx context.Context, gateway *discordv1alpha1.DiscordGateway) error {
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

// reconcileStatefulSet creates or updates the StatefulSet for the gateway.
// On update, all mutable fields (replicas, pod template, update strategy) are
// synced to the desired state so that image, resource, and env-var changes are
// not silently ignored.
func (r *DiscordGatewayReconciler) reconcileStatefulSet(ctx context.Context, gateway *discordv1alpha1.DiscordGateway, replicas int32) error {
	logger := log.FromContext(ctx)

	desiredSts := k8s.BuildStatefulSet(gateway, replicas)

	// Set owner reference on the desired object so Kubernetes garbage-collects
	// the StatefulSet when the DiscordGateway is deleted.
	if err := controllerutil.SetControllerReference(gateway, desiredSts, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	existingSts := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: desiredSts.Name, Namespace: desiredSts.Namespace}, existingSts)

	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating StatefulSet", "name", desiredSts.Name, "replicas", replicas)
		if createErr := r.Create(ctx, desiredSts); createErr != nil {
			return fmt.Errorf("failed to create StatefulSet: %w", createErr)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Determine whether any mutable field has drifted from the desired state.
	// spec.selector and spec.volumeClaimTemplates are immutable after creation
	// and must not be touched here.
	replicasDrifted := existingSts.Spec.Replicas == nil || *existingSts.Spec.Replicas != replicas

	if replicasDrifted {
		logger.Info("Updating StatefulSet", "name", desiredSts.Name, "replicas", replicas)

		// Sync all mutable fields in one update to avoid partial drifts.
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

// SetupWithManager sets up the controller with the Manager.
func (r *DiscordGatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&discordv1alpha1.DiscordGateway{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Named("discordgateway").
		Complete(r)
}
