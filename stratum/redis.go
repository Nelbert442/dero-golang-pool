package stratum

import (
	"context"
	"fmt"
	"log"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"github.com/go-redis/redis"
)

var ctx = context.Background()

func NewRedisClient(cfg *pool.Redis) {
	redisAddr := fmt.Sprintf("%s:%v", cfg.Host, cfg.Port)
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,    // Defines redis addr, usually 127.0.0.1:6379
		Password: cfg.Password, // Generally pwd blank, option if required
		DB:       cfg.DB,       // Generally set to 0 for default DB
	})

	log.Printf("Redis DB Set To: %s => Index %v", redisAddr, cfg.DB)

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatal("[Fatal] Redis DB Connection Error: ", err.Error())
	}

	log.Printf("Redis DB Connection Successful: %s", redisAddr)
}
