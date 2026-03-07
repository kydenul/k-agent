package server

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/kydenul/k-agent/internal/stores"
	"github.com/kydenul/k-agent/pb/user"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UserServer struct {
	user.UnimplementedUserServiceServer

	db  *stores.PostgresClient
	rdb *stores.RedisClient
}

// NewUserServer creates a new user server which contains PostgresClient and RedisClient.
func NewUserServer(pg *stores.PostgresClient, rdb *stores.RedisClient) *UserServer {
	return &UserServer{
		db:  pg,
		rdb: rdb,
	}
}

func (s *UserServer) GetUser(
	ctx context.Context,
	req *user.GetUserRequest,
) (*user.GetUserResponse, error) {
	u := new(stores.User)
	err := s.db.DB().NewSelect().Model(u).
		Where("id = ?", req.GetId()).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "user %s not found", req.GetId())
		}
		return nil, status.Errorf(codes.Internal, "failed to get user: %v", err)
	}

	return &user.GetUserResponse{
		User: &user.User{
			Id:        u.ID,
			Name:      u.Name,
			Email:     u.Email,
			CreatedAt: u.CreatedAt,
		},
	}, nil
}

func (s *UserServer) CreateUser(
	ctx context.Context,
	req *user.CreateUserRequest,
) (*user.CreateUserResponse, error) {
	u := &stores.User{
		ID:        uuid.New().String(),
		Name:      req.GetName(),
		Email:     req.GetEmail(),
		CreatedAt: time.Now().Unix(),
	}

	_, err := s.db.DB().NewInsert().Model(u).Exec(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create user: %v", err)
	}

	return &user.CreateUserResponse{
		User: &user.User{
			Id:        u.ID,
			Name:      u.Name,
			Email:     u.Email,
			CreatedAt: u.CreatedAt,
		},
	}, nil
}

func (s *UserServer) ListUsers(
	ctx context.Context,
	req *user.ListUsersRequest,
) (*user.ListUsersResponse, error) {
	page := int(req.GetPage())
	if page < 1 {
		page = 1
	}

	pageSize := int(req.GetPageSize())
	if pageSize < 1 {
		pageSize = 20
	}

	var users []stores.User
	count, err := s.db.DB().NewSelect().
		Model(&users).
		OrderExpr("created_at DESC").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		ScanAndCount(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list users: %v", err)
	}

	pbUsers := make([]*user.User, len(users))
	for i, u := range users {
		pbUsers[i] = &user.User{
			Id:        u.ID,
			Name:      u.Name,
			Email:     u.Email,
			CreatedAt: u.CreatedAt,
		}
	}

	return &user.ListUsersResponse{
		Users: pbUsers,
		Total: int32(min(count, math.MaxInt32)), //nolint:gosec // bounded by min
	}, nil
}

func (s *UserServer) DeleteUser(
	ctx context.Context,
	req *user.DeleteUserRequest,
) (*user.DeleteUserResponse, error) {
	res, err := s.db.DB().NewDelete().
		Model((*stores.User)(nil)).
		Where("id = ?", req.GetId()).
		Exec(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete user: %v", err)
	}

	rows, _ := res.RowsAffected()

	return &user.DeleteUserResponse{
		Success: rows > 0,
	}, nil
}
