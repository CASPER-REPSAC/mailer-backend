package auth

import (
	"context"
	"fmt"

	authentik "goauthentik.io/api/v3"
)

type AuthentikConfig struct {
	BaseURL  string
	ApiToken string
}

type AuthentikClient struct {
	client *authentik.APIClient
}

func NewAuthentikClient(cfg *AuthentikConfig) (*AuthentikClient, error) {
	if cfg.BaseURL == "" || cfg.ApiToken == "" {
		return nil, fmt.Errorf("invalid authentik config: missing BaseURL or ApiToken")
	}
	configuration := authentik.NewConfiguration()
	configuration.Servers = []authentik.ServerConfiguration{{URL: cfg.BaseURL}}
	configuration.AddDefaultHeader("Authorization", "Bearer "+cfg.ApiToken)
	apiClient := authentik.NewAPIClient(configuration)
	return &AuthentikClient{
		client: apiClient,
	}, nil
}

func (ac *AuthentikClient) GetUserList(ctx context.Context) ([]authentik.User, error) {
	resp, httpResp, err := ac.client.CoreApi.CoreUsersList(ctx).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user list, http status %s: %w", httpResp.Status, err)
	}
	filteredUsers := make([]authentik.User, 0)
	for _, user := range resp.Results {
		if user.GetType() != "internal_service_account" {
			filteredUsers = append(filteredUsers, user)
		}
	}
	return filteredUsers, nil
}

func (ac *AuthentikClient) GetUserByID(ctx context.Context, id int32) (*authentik.User, error) {
	user, httpResp, err := ac.client.CoreApi.CoreUsersRetrieve(ctx, id).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve user (ID: %d), http status %s: %w", id, httpResp.Status, err)
	}
	return user, nil
}
