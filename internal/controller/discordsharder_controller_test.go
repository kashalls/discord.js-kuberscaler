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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	discordv1alpha1 "github.com/waifulabs/discord.js-kuberscaler/api/v1alpha1"
	"github.com/waifulabs/discord.js-kuberscaler/internal/discord"
)

var _ = Describe("DiscordSharder Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		discordsharder := &discordv1alpha1.DiscordSharder{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind DiscordSharder")
			err := k8sClient.Get(ctx, typeNamespacedName, discordsharder)
			if err != nil && errors.IsNotFound(err) {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-token",
						Namespace: "default",
					},
					StringData: map[string]string{
						"token": "test-token-value",
					},
				}
				Expect(k8sClient.Create(ctx, secret)).To(Succeed())

				resource := &discordv1alpha1.DiscordSharder{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: discordv1alpha1.DiscordSharderSpec{
						TokenSecretRef: discordv1alpha1.SecretReference{
							Name: "test-token",
							Key:  "token",
						},
						Sharding: discordv1alpha1.ShardingConfig{
							FixedShardCount: func() *int32 {
								i := int32(3)
								return &i
							}(),
						},
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "bot",
										Image: "test:latest",
										Env: []corev1.EnvVar{
											{
												Name: "DISCORD_TOKEN",
												ValueFrom: &corev1.EnvVarSource{
													SecretKeyRef: &corev1.SecretKeySelector{
														LocalObjectReference: corev1.LocalObjectReference{
															Name: "test-token",
														},
														Key: "token",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &discordv1alpha1.DiscordSharder{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance DiscordSharder")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Cleanup the test-token secret")
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-token", Namespace: "default"}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should create a StatefulSet and headless Service with the correct configuration", func() {
			By("Reconciling the created resource")
			controllerReconciler := &DiscordSharderReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				DiscordClient: discord.NewMockClient(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the StatefulSet was created with the correct replica count")
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, sts)).To(Succeed())
			Expect(sts.Spec.Replicas).NotTo(BeNil())
			Expect(*sts.Spec.Replicas).To(Equal(int32(3)))

			By("Verifying the StatefulSet preserves the user's container image")
			Expect(sts.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(sts.Spec.Template.Spec.Containers[0].Image).To(Equal("test:latest"))

			By("Verifying the operator injected SHARDS and SHARD_COUNT")
			envNames := make([]string, 0)
			for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
				envNames = append(envNames, e.Name)
			}
			Expect(envNames).To(ContainElements("DISCORD_TOKEN", "SHARDS", "SHARD_COUNT"))

			By("Verifying the headless Service was created")
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, svc)).To(Succeed())
			Expect(svc.Spec.ClusterIP).To(Equal("None"))

			By("Verifying the DiscordSharder status reflects the applied shard count")
			gw := &discordv1alpha1.DiscordSharder{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, gw)).To(Succeed())
			Expect(gw.Status.AppliedShards).To(Equal(int32(3)))
		})

		It("should reject an invalid sharding config where minShards exceeds maxShards", func() {
			By("Creating a DiscordSharder with minShards > maxShards")
			invalidName := "invalid-sharding"
			invalidNSN := types.NamespacedName{Name: invalidName, Namespace: "default"}

			invalidSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid-token", Namespace: "default"},
				StringData: map[string]string{"token": "test"},
			}
			Expect(k8sClient.Create(ctx, invalidSecret)).To(Succeed())

			min, max := int32(10), int32(5)
			invalidGW := &discordv1alpha1.DiscordSharder{
				ObjectMeta: metav1.ObjectMeta{Name: invalidName, Namespace: "default"},
				Spec: discordv1alpha1.DiscordSharderSpec{
					TokenSecretRef: discordv1alpha1.SecretReference{
						Name: "invalid-token",
						Key:  "token",
					},
					Sharding: discordv1alpha1.ShardingConfig{
						MinShards: &min,
						MaxShards: &max,
					},
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "bot", Image: "test:latest"}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, invalidGW)).To(Succeed())

			controllerReconciler := &DiscordSharderReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				DiscordClient: discord.NewMockClient(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: invalidNSN})
			Expect(err).NotTo(HaveOccurred()) // returns nil, no requeue

			By("Verifying the Degraded condition is set")
			gw := &discordv1alpha1.DiscordSharder{}
			Expect(k8sClient.Get(ctx, invalidNSN, gw)).To(Succeed())

			var degradedCondition *metav1.Condition
			for i := range gw.Status.Conditions {
				if gw.Status.Conditions[i].Type == ConditionTypeDegraded {
					degradedCondition = &gw.Status.Conditions[i]
					break
				}
			}
			Expect(degradedCondition).NotTo(BeNil())
			Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue))

			Expect(k8sClient.Delete(ctx, invalidGW)).To(Succeed())
			Expect(k8sClient.Delete(ctx, invalidSecret)).To(Succeed())
		})
	})
})
