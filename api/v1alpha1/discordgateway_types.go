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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ShardingMode defines the mode for determining shard count.
// +kubebuilder:validation:Enum=Recommended;Fixed
type ShardingMode string

const (
	// ShardingModeRecommended uses Discord's recommended shard count
	ShardingModeRecommended ShardingMode = "Recommended"
	// ShardingModeFixed uses a fixed shard count
	ShardingModeFixed ShardingMode = "Fixed"
)

// UpdateStrategy controls how shard count changes are rolled out.
// +kubebuilder:validation:Enum=RollingUpdate;BlueGreen
type UpdateStrategy string

const (
	// UpdateStrategyRollingUpdate updates the StatefulSet in-place, causing a
	// rolling restart of all shard pods whenever the shard count changes.
	UpdateStrategyRollingUpdate UpdateStrategy = "RollingUpdate"
	// UpdateStrategyBlueGreen creates a second StatefulSet with the new shard
	// count, waits for all new pods to be Ready, then removes the old fleet.
	// Both fleets run concurrently during the transition; applications should
	// deduplicate events if that is a concern.
	UpdateStrategyBlueGreen UpdateStrategy = "BlueGreen"
)

// ChangeStrategy controls when shard count changes are applied.
// +kubebuilder:validation:Enum=Immediate;OnAnnotation
type ChangeStrategy string

const (
	// ChangeStrategyImmediate applies shard count changes as soon as they are
	// detected. This is the default behaviour.
	ChangeStrategyImmediate ChangeStrategy = "Immediate"
	// ChangeStrategyOnAnnotation defers shard count changes until the operator
	// sees the annotation discord.ok8.sh/allow-reshard: "true" on the
	// DiscordGateway resource. The annotation is removed automatically once the
	// change has been initiated, so it acts as a one-shot gate.
	ChangeStrategyOnAnnotation ChangeStrategy = "OnAnnotation"
)

// SecretReference contains information about a Secret containing the bot token.
type SecretReference struct {
	// Name is the name of the Secret
	Name string `json:"name"`
	// Key is the key within the Secret containing the bot token
	Key string `json:"key"`
}

// ShardingConfig defines the sharding configuration.
type ShardingConfig struct {
	// Mode determines how shard count is calculated (Recommended or Fixed)
	// +kubebuilder:default=Recommended
	Mode ShardingMode `json:"mode,omitempty"`
	// FixedShardCount is the number of shards when mode is Fixed
	// +optional
	FixedShardCount *int32 `json:"fixedShardCount,omitempty"`
	// MinShards is the minimum number of shards (used with Recommended mode)
	// +optional
	// +kubebuilder:validation:Minimum=1
	MinShards *int32 `json:"minShards,omitempty"`
	// MaxShards is the maximum number of shards (used with Recommended mode)
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxShards *int32 `json:"maxShards,omitempty"`
	// StepSize rounds the desired shard count up to the next multiple of this
	// value before applying min/max constraints. For example, StepSize=4 keeps
	// shard counts at 4, 8, 12, … reducing the frequency of full restarts as
	// Discord's recommendation grows gradually. Powers of two are recommended.
	// +optional
	// +kubebuilder:validation:Minimum=1
	StepSize *int32 `json:"stepSize,omitempty"`
	// UpdateStrategy controls how a shard count change is rolled out.
	// Defaults to RollingUpdate.
	// +optional
	// +kubebuilder:default=RollingUpdate
	UpdateStrategy UpdateStrategy `json:"updateStrategy,omitempty"`
	// ChangeStrategy controls when a detected shard count change is applied.
	// Defaults to Immediate.
	// +optional
	// +kubebuilder:default=Immediate
	ChangeStrategy ChangeStrategy `json:"changeStrategy,omitempty"`
}

// DiscordGatewaySpec defines the desired state of DiscordGateway.
type DiscordGatewaySpec struct {
	// SyncInterval controls how often the operator polls the Discord API for
	// the recommended shard count. Defaults to 10 minutes if unset.
	// +optional
	SyncInterval *metav1.Duration `json:"syncInterval,omitempty"`
	// TokenSecretRef references the Secret containing the bot token. The operator
	// reads this token solely to call Discord's /gateway/bot API to determine the
	// recommended shard count. It is not automatically injected into pods — add it
	// to spec.template.spec.containers[*].env yourself (via secretKeyRef or
	// an ExternalSecret).
	TokenSecretRef SecretReference `json:"tokenSecretRef"`

	// Sharding defines how the shard count is determined.
	// +optional
	Sharding ShardingConfig `json:"sharding,omitempty"`

	// Template is the pod template used for every shard pod. The operator sets
	// spec.replicas on the resulting StatefulSet and injects two environment
	// variables into every container (unless already declared by the user):
	//
	//   SHARDS      – numeric ordinal of the pod; discord.js v14 reads this natively
	//   SHARD_COUNT – total number of shards; discord.js v14 reads this natively
	//
	// Everything else — container image, resource limits, volumes, secrets,
	// additional env vars — is the user's responsibility inside this template.
	Template corev1.PodTemplateSpec `json:"template"`
}

// DiscordGatewayStatus defines the observed state of DiscordGateway.
type DiscordGatewayStatus struct {
	// RecommendedShards is the shard count recommended by Discord
	// +optional
	RecommendedShards int32 `json:"recommendedShards,omitempty"`
	// AppliedShards is the actual number of shards currently deployed
	// +optional
	AppliedShards int32 `json:"appliedShards,omitempty"`
	// MaxConcurrency is the max_concurrency value from Discord
	// +optional
	MaxConcurrency int32 `json:"maxConcurrency,omitempty"`
	// LastSyncTime is the last time the operator synced with Discord API
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
	// Conditions represent the latest available observations of the DiscordGateway's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ActiveStatefulSet is the name of the currently serving StatefulSet.
	// Only set when UpdateStrategy is BlueGreen; empty means the default name
	// (same as the DiscordGateway) is active.
	// +optional
	ActiveStatefulSet string `json:"activeStatefulSet,omitempty"`
	// PendingShards is the desired shard count that has not yet been applied.
	// Non-zero when a change is held by ChangeStrategy=OnAnnotation, or while a
	// BlueGreen transition is in progress.
	// +optional
	PendingShards int32 `json:"pendingShards,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.sharding.mode`
// +kubebuilder:printcolumn:name="Applied Shards",type=integer,JSONPath=`.status.appliedShards`
// +kubebuilder:printcolumn:name="Recommended",type=integer,JSONPath=`.status.recommendedShards`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DiscordGateway is the Schema for the discordgateways API.
type DiscordGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DiscordGatewaySpec   `json:"spec,omitempty"`
	Status DiscordGatewayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DiscordGatewayList contains a list of DiscordGateway.
type DiscordGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DiscordGateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DiscordGateway{}, &DiscordGatewayList{})
}
