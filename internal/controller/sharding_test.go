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
		gateway           *discordv1alpha1.DiscordSharder
		recommendedShards int32
		expected          int32
	}{
		{
			name: "no constraints uses recommended",
			gateway: &discordv1alpha1.DiscordSharder{
				Spec: discordv1alpha1.DiscordSharderSpec{
					Sharding: discordv1alpha1.ShardingConfig{},
				},
			},
			recommendedShards: 10,
			expected:          10,
		},
		{
			name: "min constraint raises recommended",
			gateway: &discordv1alpha1.DiscordSharder{
				Spec: discordv1alpha1.DiscordSharderSpec{
					Sharding: discordv1alpha1.ShardingConfig{
						MinShards: int32Ptr(5),
					},
				},
			},
			recommendedShards: 3,
			expected:          5,
		},
		{
			name: "max constraint caps recommended",
			gateway: &discordv1alpha1.DiscordSharder{
				Spec: discordv1alpha1.DiscordSharderSpec{
					Sharding: discordv1alpha1.ShardingConfig{
						MaxShards: int32Ptr(8),
					},
				},
			},
			recommendedShards: 12,
			expected:          8,
		},
		{
			name: "both constraints, recommended within range",
			gateway: &discordv1alpha1.DiscordSharder{
				Spec: discordv1alpha1.DiscordSharderSpec{
					Sharding: discordv1alpha1.ShardingConfig{
						MinShards: int32Ptr(5),
						MaxShards: int32Ptr(15),
					},
				},
			},
			recommendedShards: 10,
			expected:          10,
		},
		{
			name: "fixedShardCount ignores recommended",
			gateway: &discordv1alpha1.DiscordSharder{
				Spec: discordv1alpha1.DiscordSharderSpec{
					Sharding: discordv1alpha1.ShardingConfig{
						FixedShardCount: int32Ptr(7),
					},
				},
			},
			recommendedShards: 10,
			expected:          7,
		},
		{
			name: "step-size rounds up to next multiple",
			gateway: &discordv1alpha1.DiscordSharder{
				Spec: discordv1alpha1.DiscordSharderSpec{
					Sharding: discordv1alpha1.ShardingConfig{
						StepSize: int32Ptr(4),
					},
				},
			},
			recommendedShards: 5,
			expected:          8,
		},
		{
			name: "step-size is a no-op when already aligned",
			gateway: &discordv1alpha1.DiscordSharder{
				Spec: discordv1alpha1.DiscordSharderSpec{
					Sharding: discordv1alpha1.ShardingConfig{
						StepSize: int32Ptr(4),
					},
				},
			},
			recommendedShards: 8,
			expected:          8,
		},
		{
			name: "step-size of 1 is a no-op",
			gateway: &discordv1alpha1.DiscordSharder{
				Spec: discordv1alpha1.DiscordSharderSpec{
					Sharding: discordv1alpha1.ShardingConfig{
						StepSize: int32Ptr(1),
					},
				},
			},
			recommendedShards: 7,
			expected:          7,
		},
		{
			name: "step-size rounding applied before max constraint",
			gateway: &discordv1alpha1.DiscordSharder{
				Spec: discordv1alpha1.DiscordSharderSpec{
					Sharding: discordv1alpha1.ShardingConfig{
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
			r := &DiscordSharderReconciler{}
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
