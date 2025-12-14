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

package k8s

import (
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	discordv1alpha1 "github.com/waifulabs/discord.js-kuberscaler/api/v1alpha1"
)

const (
	appLabel = "discord-gateway"
)

// BuildStatefulSet creates a StatefulSet for the Discord gateway shards.
func BuildStatefulSet(gateway *discordv1alpha1.DiscordGateway, replicas int32, maxConcurrency int32) *appsv1.StatefulSet {
	labels := map[string]string{
		"app":                          appLabel,
		"discord.nerdz.io/gateway":     gateway.Name,
		"app.kubernetes.io/name":       "discord-gateway",
		"app.kubernetes.io/instance":   gateway.Name,
		"app.kubernetes.io/managed-by": "discord-gateway-operator",
	}

	// Calculate intents bitmask if needed
	// For now, we'll pass it as a simple string or value
	// The actual bot implementation should handle intent calculation
	intents := "0"
	if gateway.Spec.Intents.Privileged {
		intents = "privileged"
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			ServiceName: gateway.Name,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "gateway",
							Image: gateway.Spec.Image,
							Env: []corev1.EnvVar{
								{
									Name:  "DISCORD_SHARD_COUNT",
									Value: strconv.Itoa(int(replicas)),
								},
								{
									Name: "DISCORD_SHARD_ID",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								},
								{
									Name:  "DISCORD_MAX_CONCURRENCY",
									Value: strconv.Itoa(int(maxConcurrency)),
								},
								{
									Name:  "DISCORD_INTENTS",
									Value: intents,
								},
								{
									Name: "DISCORD_TOKEN",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: gateway.Spec.TokenSecretRef.Name,
											},
											Key: gateway.Spec.TokenSecretRef.Key,
										},
									},
								},
							},
							Resources: gateway.Spec.PodTemplate.Resources,
						},
					},
				},
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable: func() *intstr.IntOrString {
						val := intstr.FromInt(1)
						return &val
					}(),
				},
			},
		},
	}

	return statefulSet
}

// ExtractShardIDFromPodName extracts the shard ID from a pod name.
// Pod names in StatefulSets are formatted as <statefulset-name>-<ordinal>.
func ExtractShardIDFromPodName(podName string) (int32, error) {
	// This is a helper function that could be used in init containers
	// or by the bot application itself to parse the shard ID
	var ordinal int
	_, err := fmt.Sscanf(podName, "%*[^-]-%d", &ordinal)
	if err != nil {
		return 0, fmt.Errorf("failed to parse shard ID from pod name %s: %w", podName, err)
	}
	return int32(ordinal), nil
}
