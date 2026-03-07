package client

import (
	"context"
	"fmt"
	"time"

	"github.com/kydenul/k-agent/pb/user"
	"github.com/kydenul/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// UserClient wraps the gRPC UserServiceClient with connection management.
type UserClient struct {
	conn   *grpc.ClientConn
	client user.UserServiceClient
}

// NewUserClient creates a new gRPC client connection to the user service.
func NewUserClient(addr string) (*UserClient, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		log.Errorf("failed to dial: %v", err)
		return nil, fmt.Errorf("failed to connect to user service at %s: %w", addr, err)
	}

	return &UserClient{
		conn:   conn,
		client: user.NewUserServiceClient(conn),
	}, nil
}

// Close releases the underlying connection.
func (c *UserClient) Close() error { return c.conn.Close() }

// ========== Wrapped Methods ==========

func (c *UserClient) GetUser(ctx context.Context, id string) (*user.User, error) {
	resp, err := c.client.GetUser(ctx, &user.GetUserRequest{Id: id})
	if err != nil {
		log.Errorf("failed to get user: %v", err)
		return nil, fmt.Errorf("GetUser rpc: %w", err)
	}

	log.Infof("GetUser: %v", resp.User)

	return resp.User, nil
}

func (c *UserClient) CreateUser(ctx context.Context, name, email string) (*user.User, error) {
	resp, err := c.client.CreateUser(ctx, &user.CreateUserRequest{Name: name, Email: email})
	if err != nil {
		log.Errorf("failed to create user: %v", err)
		return nil, fmt.Errorf("CreateUser rpc: %w", err)
	}

	log.Infof("CreateUser: %v", resp.User)

	return resp.User, nil
}

func (c *UserClient) ListUsers(
	ctx context.Context,
	page, pageSize int32,
) ([]*user.User, int32, error) {
	resp, err := c.client.ListUsers(ctx, &user.ListUsersRequest{Page: page, PageSize: pageSize})
	if err != nil {
		log.Errorf("failed to list users: %v", err)
		return nil, 0, fmt.Errorf("ListUsers rpc: %w", err)
	}

	log.Infof("ListUsers: %v", resp.Users)

	return resp.Users, resp.Total, nil
}

func (c *UserClient) DeleteUser(ctx context.Context, id string) (bool, error) {
	resp, err := c.client.DeleteUser(ctx, &user.DeleteUserRequest{Id: id})
	if err != nil {
		log.Errorf("failed to delete user: %v", err)
		return false, fmt.Errorf("DeleteUser rpc: %w", err)
	}

	log.Infof("DeleteUser: %v", resp.Success)

	return resp.Success, nil
}
