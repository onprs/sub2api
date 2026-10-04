package zcode

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"time"
)

type RedisStore struct{ Client *redis.Client }

func (s *RedisStore) Get(ctx context.Context, key string) (string, error) {
	value, err := s.Client.Get(ctx, "zcode:"+key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return value, err
}
func (s *RedisStore) Put(ctx context.Context, key, value string, ttl time.Duration) error {
	return s.Client.Set(ctx, "zcode:"+key, value, ttl).Err()
}
func (s *RedisStore) Take(ctx context.Context, key string) (string, error) {
	value, err := s.Client.GetDel(ctx, "zcode:"+key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return value, err
}
func (s *RedisStore) Acquire(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	return s.Client.SetNX(ctx, "zcode:"+key, owner, ttl).Result()
}
func (s *RedisStore) Release(ctx context.Context, key, owner string) error {
	return s.Client.Eval(ctx, "if redis.call('get',KEYS[1]) == ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end", []string{"zcode:" + key}, owner).Err()
}
