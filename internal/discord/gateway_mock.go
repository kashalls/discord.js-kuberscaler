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

package discord

import (
	"context"
	"fmt"
)

// Ensure MockClient implements GatewayClient interface.
var _ GatewayClient = (*MockClient)(nil)

// MockClient is a mock implementation of the Discord client for testing.
type MockClient struct {
	GetGatewayBotFunc func(ctx context.Context, token string) (*GatewayBotResponse, error)
}

// NewMockClient creates a new mock Discord client.
func NewMockClient() *MockClient {
	return &MockClient{
		GetGatewayBotFunc: func(ctx context.Context, token string) (*GatewayBotResponse, error) {
			return &GatewayBotResponse{
				URL:    "wss://gateway.discord.gg",
				Shards: 1,
				SessionStartLimit: SessionStartLimit{
					Total:          1000,
					Remaining:      999,
					ResetAfter:     86400000,
					MaxConcurrency: 1,
				},
			}, nil
		},
	}
}

// GetGatewayBot mocks the GetGatewayBot method.
func (m *MockClient) GetGatewayBot(ctx context.Context, token string) (*GatewayBotResponse, error) {
	if m.GetGatewayBotFunc != nil {
		return m.GetGatewayBotFunc(ctx, token)
	}
	return nil, fmt.Errorf("GetGatewayBotFunc not implemented")
}
