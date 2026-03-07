package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/kydenul/k-agent/internal/stores"
	"github.com/kydenul/k-agent/pb/user"
	"github.com/kydenul/log"
	"github.com/spf13/cast"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	userCacheTTL = 5 * time.Minute
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

func (s *UserServer) userRedisKey(id string) string {
	return "k-agent:user:" + id
}

func (s *UserServer) userListRedisKey(page, pageSize int) string {
	return fmt.Sprintf("k-agent:user:list:%d:%d", page, pageSize)
}

const userListRedisKeyPattern = "k-agent:user:list:*"

// invalidateListCache removes all cached user list pages.
func (s *UserServer) invalidateListCache(ctx context.Context) {
	iter := s.rdb.Scan(ctx, 0, userListRedisKeyPattern, 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if len(keys) > 0 {
		if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
			log.Warnf("failed to invalidate user list cache: %v", err)
		}
	}
}

func userToProto(u *stores.User) *user.User {
	return &user.User{
		Id:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		CreatedAt: u.CreatedAt.Format(time.DateTime),
	}
}

func (s *UserServer) GetUser(
	ctx context.Context,
	req *user.GetUserRequest,
) (*user.GetUserResponse, error) {
	// Try to get user from Redis cache first
	if val, err := s.rdb.Get(ctx, s.userRedisKey(req.GetId())).Result(); err == nil {
		u := new(stores.User)
		if err := sonic.UnmarshalString(val, u); err == nil {
			return &user.GetUserResponse{User: userToProto(u)}, nil
		}

		log.Warnf("failed to unmarshal user: %v, it will get from database", err)
	}

	// Get user from database
	u := new(stores.User)
	err := s.db.DB().NewSelect().Model(u).
		Where("id = ?", req.GetId()).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "user %s not found", req.GetId())
		}
		return nil, status.Errorf(codes.Internal, "failed to get user: %v", err)
	}

	ret := &user.GetUserResponse{
		User: userToProto(u),
	}

	// Set user to Redis cache
	us, err := sonic.MarshalString(u)
	if err != nil {
		log.Warnf("failed to marshal user: %v", err)
		return ret, nil
	}

	if err := s.rdb.Set(ctx, s.userRedisKey(req.GetId()), us, userCacheTTL).Err(); err != nil {
		log.Warnf("failed to set user to Redis cache: %v", err)
		return ret, nil
	}

	return ret, nil
}

func (s *UserServer) CreateUser(
	ctx context.Context,
	req *user.CreateUserRequest,
) (*user.CreateUserResponse, error) {
	u := &stores.User{
		ID:        uuid.New().String(),
		Name:      req.GetName(),
		Email:     req.GetEmail(),
		CreatedAt: time.Now(),
	}

	_, err := s.db.DB().NewInsert().Model(u).Exec(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create user: %v", err)
	}

	s.invalidateListCache(ctx)

	return &user.CreateUserResponse{
		User: userToProto(u),
	}, nil
}

type listUsersCache struct {
	Users []stores.User `json:"users"`
	Total int           `json:"total"`
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

	cacheKey := s.userListRedisKey(page, pageSize)

	// Try to get from Redis cache first
	if val, err := s.rdb.Get(ctx, cacheKey).Result(); err == nil {
		cached := new(listUsersCache)
		if err := sonic.UnmarshalString(val, cached); err == nil {
			pbUsers := make([]*user.User, len(cached.Users))
			for i := range cached.Users {
				pbUsers[i] = userToProto(&cached.Users[i])
			}

			return &user.ListUsersResponse{
				Users: pbUsers,
				Total: cast.ToInt32(cached.Total),
			}, nil
		}

		log.Warnf("failed to unmarshal user list: %v, it will get from database", err)
	}

	// Get from database
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
	for i := range users {
		pbUsers[i] = userToProto(&users[i])
	}

	ret := &user.ListUsersResponse{
		Users: pbUsers,
		Total: cast.ToInt32(count),
	}

	// Set to Redis cache
	cached, err := sonic.MarshalString(&listUsersCache{Users: users, Total: count})
	if err != nil {
		log.Warnf("failed to marshal user list: %v", err)
		return ret, nil
	}

	if err := s.rdb.Set(ctx, cacheKey, cached, userCacheTTL).Err(); err != nil {
		log.Warnf("failed to set user list to Redis cache: %v", err)
	}

	return ret, nil
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

	if rows > 0 {
		s.rdb.Del(ctx, s.userRedisKey(req.GetId()))
		s.invalidateListCache(ctx)
	}

	return &user.DeleteUserResponse{
		Success: rows > 0,
	}, nil
}
