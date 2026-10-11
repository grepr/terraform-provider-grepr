package client

import (
	"context"
	"net/http"

	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
)

func (c *Client) CreateDataset(ctx context.Context, body generated.DatasetCreate) (*generated.DatasetRead, error) {
	var result generated.DatasetRead
	if err := c.call(false, func(a *api) (*http.Response, error) { return a.CreateDataset(ctx, body) }, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetDataset(ctx context.Context, id string) (*generated.DatasetRead, error) {
	var result generated.DatasetRead
	if err := c.call(true, func(a *api) (*http.Response, error) { return a.GetDatasetById(ctx, id) }, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) UpdateDataset(ctx context.Context, id string, body generated.DatasetUpdate) (*generated.DatasetRead, error) {
	var result generated.DatasetRead
	if err := c.call(false, func(a *api) (*http.Response, error) { return a.UpdateDataset(ctx, id, body) }, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) DeleteDataset(ctx context.Context, id string) error {
	return c.call(false, func(a *api) (*http.Response, error) { return a.DeleteDataset(ctx, id) }, nil)
}
