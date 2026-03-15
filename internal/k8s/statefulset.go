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
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	discordv1alpha1 "github.com/waifulabs/discord.js-kuberscaler/api/v1alpha1"
)

// operatorLabels returns the stable labels the operator uses for the StatefulSet
// selector and the headless Service selector. These are merged into the pod
// template labels so the selector always matches.
func operatorLabels(gatewayName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "discord-gateway-operator",
		"discord.ok8.sh/gateway":     gatewayName,
	}
}

// BuildStatefulSet creates a StatefulSet for the Discord gateway shards.
//
// The user's spec.template is used as-is for the pod spec. The operator merges
// its selector labels into the pod template labels and injects two environment
// variables into every container (only if the user has not already declared them):
//
//   - SHARDS      – numeric ordinal of the pod, sourced from the
//     apps.kubernetes.io/pod-index label (requires Kubernetes 1.28+).
//     discord.js v14 reads this directly via JSON.parse(process.env.SHARDS).
//   - SHARD_COUNT – total replica count (integer string).
//     discord.js v14 reads this directly via process.env.SHARD_COUNT.
//
// No init containers, volumes, or other mutations are added to the user's template.
func BuildStatefulSet(gateway *discordv1alpha1.DiscordGateway, replicas int32) *appsv1.StatefulSet {
	sel := operatorLabels(gateway.Name)

	// Deep-copy the user's pod template so we don't mutate the in-memory CR.
	template := gateway.Spec.Template.DeepCopy()

	// Merge operator selector labels into the pod template labels. We do not
	// replace user labels; we only add our own so the selector always matches.
	if template.Labels == nil {
		template.Labels = make(map[string]string)
	}
	for k, v := range sel {
		template.Labels[k] = v
	}

	// Inject operator-managed env vars into every container. upsertEnvVar leaves
	// user-declared values untouched, so users can override any of these.
	for i := range template.Spec.Containers {
		env := template.Spec.Containers[i].Env
		env = upsertEnvVar(env, corev1.EnvVar{
			Name: "SHARDS",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.labels['apps.kubernetes.io/pod-index']",
				},
			},
		})
		env = upsertEnvVar(env, corev1.EnvVar{
			Name:  "SHARD_COUNT",
			Value: strconv.Itoa(int(replicas)),
		})
		template.Spec.Containers[i].Env = env
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:            &replicas,
			ServiceName:         gateway.Name,
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Selector: &metav1.LabelSelector{
				MatchLabels: sel,
			},
			Template: *template,
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
}

// BuildHeadlessService creates the headless Service required by the StatefulSet.
// StatefulSets use the headless Service to give each pod a stable DNS name of
// the form <pod>.<service>.<namespace>.svc.cluster.local.
func BuildHeadlessService(gateway *discordv1alpha1.DiscordGateway) *corev1.Service {
	sel := operatorLabels(gateway.Name)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
		},
		Spec: corev1.ServiceSpec{
			// ClusterIP: "None" makes this a headless service — no virtual IP is
			// allocated and DNS returns individual pod addresses directly.
			ClusterIP: "None",
			Selector:  sel,
		},
	}
}

// upsertEnvVar appends toAdd to envs only if no env var with that name already
// exists. This lets users override operator-injected defaults by declaring the
// variable themselves in spec.template.spec.containers[*].env.
func upsertEnvVar(envs []corev1.EnvVar, toAdd corev1.EnvVar) []corev1.EnvVar {
	for _, e := range envs {
		if e.Name == toAdd.Name {
			return envs
		}
	}
	return append(envs, toAdd)
}
