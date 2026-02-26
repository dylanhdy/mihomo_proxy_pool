package db

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"

	"github.com/redis/go-redis/v9"
)

// RedisClient 封装了 Redis 操作
type RedisClient struct {
	name   string
	client *redis.Client
	ctx    context.Context
}

// NewRedisClientFromURL 通过 Redis 连接字符串初始化 RedisClient
func NewRedisClientFromURL(name string, redisURL string) (*RedisClient, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("Parse Redis URL failed: %v", err)
	}
	client := redis.NewClient(opt)

	return &RedisClient{
		name:   name,
		client: client,
		ctx:    context.Background(),
	}, nil
}

// NewRedisClient 初始化 RedisClient
func NewRedisClient(name string, options *redis.Options) *RedisClient {
	return &RedisClient{
		name:   name,
		client: redis.NewClient(options),
		ctx:    context.Background(),
	}
}

// Get 获取指定键的值
func (r *RedisClient) Get(key string) (string, error) {
	return r.client.HGet(r.ctx, r.name, key).Result()
}

// GetRandom 随机获取一个值
func (r *RedisClient) GetRandom() (string, error) {
	keys, err := r.client.HKeys(r.ctx, r.name).Result()
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", nil
	}
	randomKey := keys[rand.Intn(len(keys))]
	return r.client.HGet(r.ctx, r.name, randomKey).Result()
}

// Put 设置键值对
func (r *RedisClient) Put(key string, val any) error {
	var data string
	switch v := val.(type) {
	case string:
		data = v
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return err
		}
		data = string(bytes)
	}
	return r.client.HSet(r.ctx, r.name, key, data).Err()
}

// Delete 删除指定键
func (r *RedisClient) Delete(key string) error {
	return r.client.HDel(r.ctx, r.name, key).Err()
}

// Exists 检查键是否存在
func (r *RedisClient) Exists(key string) bool {
	ret, err := r.client.HExists(r.ctx, r.name, key).Result()
	if err != nil {
		return false
	}
	return ret
}

// GetAllValues 获取所有值
func (r *RedisClient) GetAllValues() ([]string, error) {
	values, err := r.client.HVals(r.ctx, r.name).Result()
	if err != nil {
		return nil, err
	}
	return values, nil
}

// GetAll 获取所有键值对
func (r *RedisClient) GetAll() (map[string]string, error) {
	items, err := r.client.HGetAll(r.ctx, r.name).Result()
	if err != nil {
		return nil, err
	}
	return items, nil
}

// GetAllByPrefix 获取指定前缀的键值对
func (r *RedisClient) GetAllByPrefix(prefix string) (map[string]string, error) {
	ret := make(map[string]string)
	var cursor uint64
	for {
		items, nextCursor, err := r.client.HScan(r.ctx, r.name, cursor, prefix+"*", 100).Result()
		if err != nil {
			return nil, err
		}

		for i := 0; i+1 < len(items); i += 2 {
			key := items[i]
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			ret[key] = items[i+1]
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return ret, nil
}

// Clear 清空哈希表
func (r *RedisClient) Clear() error {
	return r.client.Del(r.ctx, r.name).Err()
}

// GetCount 获取键值对数量
func (r *RedisClient) GetCount() (int64, error) {
	return r.client.HLen(r.ctx, r.name).Result()
}

// ChangeTable 更改哈希表名称
func (r *RedisClient) ChangeTable(name string) {
	r.name = name
}
