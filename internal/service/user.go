package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/kydenul/k-agent/internal/stores"
	"github.com/kydenul/log"
	"github.com/spf13/cast"
)

const (
	userCacheTTL = 5 * time.Minute
)

var ErrUserNotFound = errors.New("user not found")

type UserResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

type ListUsersResponse struct {
	Users []*UserResponse `json:"users"`
	Total int32           `json:"total"`
}

type UserService struct {
	db  *stores.PostgresClient
	rdb *stores.RedisClient
}

func NewUserService(pg *stores.PostgresClient, rdb *stores.RedisClient) *UserService {
	return &UserService{
		db:  pg,
		rdb: rdb,
	}
}

func (s *UserService) userRedisKey(id string) string {
	return "k-agent:user:" + id
}

func (s *UserService) userListRedisKey(page, pageSize int) string {
	return fmt.Sprintf("k-agent:user:list:%d:%d", page, pageSize)
}

const userListRedisKeyPattern = "k-agent:user:list:*"

func (s *UserService) invalidateListCache(ctx context.Context) {
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

func userToResponse(u *stores.User) *UserResponse {
	return &UserResponse{
		ID:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		CreatedAt: u.CreatedAt.Format(time.DateTime),
	}
}

func (s *UserService) GetUser(ctx context.Context, id string) (*UserResponse, error) {
	// Try Redis cache first
	if val, err := s.rdb.Get(ctx, s.userRedisKey(id)).Result(); err == nil {
		u := new(stores.User)
		if err := sonic.UnmarshalString(val, u); err == nil {
			return userToResponse(u), nil
		}

		log.Warnf("failed to unmarshal user: %v, it will get from database", err)
	}

	// Get from database
	u := new(stores.User)
	err := s.db.DB().NewSelect().Model(u).
		Where("id = ?", id).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Set to Redis cache
	us, err := sonic.MarshalString(u)
	if err != nil {
		log.Warnf("failed to marshal user: %v", err)
		return userToResponse(u), nil
	}

	if err := s.rdb.Set(ctx, s.userRedisKey(id), us, userCacheTTL).Err(); err != nil {
		log.Warnf("failed to set user to Redis cache: %v", err)
	}

	return userToResponse(u), nil
}

func (s *UserService) CreateUser(ctx context.Context, name, email string) (*UserResponse, error) {
	u := &stores.User{
		ID:        uuid.New().String(),
		Name:      name,
		Email:     email,
		CreatedAt: time.Now(),
	}

	_, err := s.db.DB().NewInsert().Model(u).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	s.invalidateListCache(ctx)

	return userToResponse(u), nil
}

type listUsersCache struct {
	Users []stores.User `json:"users"`
	Total int           `json:"total"`
}

func (s *UserService) ListUsers(
	ctx context.Context,
	page, pageSize int,
) (*ListUsersResponse, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	cacheKey := s.userListRedisKey(page, pageSize)

	// Try Redis cache first
	if val, err := s.rdb.Get(ctx, cacheKey).Result(); err == nil {
		cached := new(listUsersCache)
		if err := sonic.UnmarshalString(val, cached); err == nil {
			users := make([]*UserResponse, len(cached.Users))
			for i := range cached.Users {
				users[i] = userToResponse(&cached.Users[i])
			}

			return &ListUsersResponse{
				Users: users,
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
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	resp := make([]*UserResponse, len(users))
	for i := range users {
		resp[i] = userToResponse(&users[i])
	}

	ret := &ListUsersResponse{
		Users: resp,
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

func (s *UserService) DeleteUser(ctx context.Context, id string) (bool, error) {
	res, err := s.db.DB().NewDelete().
		Model((*stores.User)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to delete user: %w", err)
	}

	rows, _ := res.RowsAffected()

	if rows > 0 {
		s.rdb.Del(ctx, s.userRedisKey(id))
		s.invalidateListCache(ctx)
	}

	return rows > 0, nil
}
