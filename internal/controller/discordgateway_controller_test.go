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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	discordv1alpha1 "github.com/waifulabs/discord.js-kuberscaler/api/v1alpha1"
	"github.com/waifulabs/discord.js-kuberscaler/internal/discord"
)

var _ = Describe("DiscordGateway Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		discordgateway := &discordv1alpha1.DiscordGateway{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind DiscordGateway")
			err := k8sClient.Get(ctx, typeNamespacedName, discordgateway)
			if err != nil && errors.IsNotFound(err) {
				// Create a test Secret first
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

				resource := &discordv1alpha1.DiscordGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: discordv1alpha1.DiscordGatewaySpec{
						Image: "test:latest",
						TokenSecretRef: discordv1alpha1.SecretReference{
							Name: "test-token",
							Key:  "token",
						},
						Sharding: discordv1alpha1.ShardingConfig{
							Mode: discordv1alpha1.ShardingModeFixed,
							FixedShardCount: func() *int32 {
								i := int32(1)
								return &i
							}(),
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &discordv1alpha1.DiscordGateway{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance DiscordGateway")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			// Create a mock Discord client
			mockClient := discord.NewMockClient()
			mockClient.GetGatewayBotFunc = func(ctx context.Context, token string) (*discord.GatewayBotResponse, error) {
				return &discord.GatewayBotResponse{
					URL:    "wss://gateway.discord.gg",
					Shards: 1,
					SessionStartLimit: discord.SessionStartLimit{
						Total:          1000,
						Remaining:      999,
						ResetAfter:     86400000,
						MaxConcurrency: 1,
					},
				}, nil
			}

			controllerReconciler := &DiscordGatewayReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				DiscordClient: mockClient,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
