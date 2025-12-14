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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ShardingMode defines the mode for determining shard count.
// +kubebuilder:validation:Enum=Recommended;Fixed
type ShardingMode string

const (
	// ShardingModeRecommended uses Discord's recommended shard count
	ShardingModeRecommended ShardingMode = "Recommended"
	// ShardingModeFixed uses a fixed shard count
	ShardingModeFixed ShardingMode = "Fixed"
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
}

// IntentsConfig defines Discord intent configuration.
type IntentsConfig struct {
	// Privileged indicates whether privileged intents are enabled
	// +optional
	Privileged bool `json:"privileged,omitempty"`
}

// PodTemplate defines the pod template configuration.
type PodTemplate struct {
	// Resources defines the resource requirements for shard pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// DiscordGatewaySpec defines the desired state of DiscordGateway.
type DiscordGatewaySpec struct {
	// Image is the container image for shard pods
	Image string `json:"image"`
	// TokenSecretRef references the Secret containing the bot token
	TokenSecretRef SecretReference `json:"tokenSecretRef"`
	// Sharding defines the sharding configuration
	// +optional
	Sharding ShardingConfig `json:"sharding,omitempty"`
	// Intents defines the Discord intents configuration
	// +optional
	Intents IntentsConfig `json:"intents,omitempty"`
	// PodTemplate defines the pod template configuration
	// +optional
	PodTemplate PodTemplate `json:"podTemplate,omitempty"`
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
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
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
