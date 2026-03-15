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
	"testing"

	discordv1alpha1 "github.com/waifulabs/discord.js-kuberscaler/api/v1alpha1"
)

func TestCalculateDesiredShards(t *testing.T) {
	tests := []struct {
		name              string
		gateway           *discordv1alpha1.DiscordGateway
		recommendedShards int32
		expected          int32
	}{
		{
			name: "Recommended mode with no constraints",
			gateway: &discordv1alpha1.DiscordGateway{
				Spec: discordv1alpha1.DiscordGatewaySpec{
					Sharding: discordv1alpha1.ShardingConfig{
						Mode: discordv1alpha1.ShardingModeRecommended,
					},
				},
			},
			recommendedShards: 10,
			expected:          10,
		},
		{
			name: "Recommended mode with min constraint",
			gateway: &discordv1alpha1.DiscordGateway{
				Spec: discordv1alpha1.DiscordGatewaySpec{
					Sharding: discordv1alpha1.ShardingConfig{
						Mode:      discordv1alpha1.ShardingModeRecommended,
						MinShards: int32Ptr(5),
					},
				},
			},
			recommendedShards: 3,
			expected:          5,
		},
		{
			name: "Recommended mode with max constraint",
			gateway: &discordv1alpha1.DiscordGateway{
				Spec: discordv1alpha1.DiscordGatewaySpec{
					Sharding: discordv1alpha1.ShardingConfig{
						Mode:      discordv1alpha1.ShardingModeRecommended,
						MaxShards: int32Ptr(8),
					},
				},
			},
			recommendedShards: 12,
			expected:          8,
		},
		{
			name: "Recommended mode with both constraints",
			gateway: &discordv1alpha1.DiscordGateway{
				Spec: discordv1alpha1.DiscordGatewaySpec{
					Sharding: discordv1alpha1.ShardingConfig{
						Mode:      discordv1alpha1.ShardingModeRecommended,
						MinShards: int32Ptr(5),
						MaxShards: int32Ptr(15),
					},
				},
			},
			recommendedShards: 10,
			expected:          10,
		},
		{
			name: "Fixed mode",
			gateway: &discordv1alpha1.DiscordGateway{
				Spec: discordv1alpha1.DiscordGatewaySpec{
					Sharding: discordv1alpha1.ShardingConfig{
						Mode:            discordv1alpha1.ShardingModeFixed,
						FixedShardCount: int32Ptr(7),
					},
				},
			},
			recommendedShards: 10,
			expected:          7,
		},
		{
			name: "Fixed mode without count defaults to 1",
			gateway: &discordv1alpha1.DiscordGateway{
				Spec: discordv1alpha1.DiscordGatewaySpec{
					Sharding: discordv1alpha1.ShardingConfig{
						Mode: discordv1alpha1.ShardingModeFixed,
					},
				},
			},
			recommendedShards: 10,
			expected:          1,
		},
		{
			name: "Empty mode defaults to Recommended",
			gateway: &discordv1alpha1.DiscordGateway{
				Spec: discordv1alpha1.DiscordGatewaySpec{
					Sharding: discordv1alpha1.ShardingConfig{},
				},
			},
			recommendedShards: 5,
			expected:          5,
		},
		{
			name: "Step-size rounds up to next multiple",
			gateway: &discordv1alpha1.DiscordGateway{
				Spec: discordv1alpha1.DiscordGatewaySpec{
					Sharding: discordv1alpha1.ShardingConfig{
						Mode:     discordv1alpha1.ShardingModeRecommended,
						StepSize: int32Ptr(4),
					},
				},
			},
			recommendedShards: 5,
			expected:          8,
		},
		{
			name: "Step-size is a no-op when already aligned",
			gateway: &discordv1alpha1.DiscordGateway{
				Spec: discordv1alpha1.DiscordGatewaySpec{
					Sharding: discordv1alpha1.ShardingConfig{
						Mode:     discordv1alpha1.ShardingModeRecommended,
						StepSize: int32Ptr(4),
					},
				},
			},
			recommendedShards: 8,
			expected:          8,
		},
		{
			name: "Step-size of 1 is a no-op",
			gateway: &discordv1alpha1.DiscordGateway{
				Spec: discordv1alpha1.DiscordGatewaySpec{
					Sharding: discordv1alpha1.ShardingConfig{
						Mode:     discordv1alpha1.ShardingModeRecommended,
						StepSize: int32Ptr(1),
					},
				},
			},
			recommendedShards: 7,
			expected:          7,
		},
		{
			name: "Step-size rounding is applied before max constraint",
			gateway: &discordv1alpha1.DiscordGateway{
				Spec: discordv1alpha1.DiscordGatewaySpec{
					Sharding: discordv1alpha1.ShardingConfig{
						Mode:      discordv1alpha1.ShardingModeRecommended,
						StepSize:  int32Ptr(4),
						MaxShards: int32Ptr(6),
					},
				},
			},
			recommendedShards: 5,
			// rounds 5 → 8, then max clips to 6
			expected: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DiscordGatewayReconciler{}
			result := r.calculateDesiredShards(tt.gateway, tt.recommendedShards)
			if result != tt.expected {
				t.Errorf("calculateDesiredShards() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

func int32Ptr(i int32) *int32 {
	return &i
}
