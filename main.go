package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/stratum"

	"github.com/go-redis/redis"
	"github.com/goji/httpauth"
	"github.com/gorilla/mux"
)

var cfg pool.Config
var ctx = context.Background()

func startStratum() string {
	if cfg.Threads > 0 {
		runtime.GOMAXPROCS(cfg.Threads)
		log.Printf("Running with %v threads", cfg.Threads)
	} else {
		n := runtime.NumCPU()
		runtime.GOMAXPROCS(n)
		log.Printf("Running with default %v threads", n)
	}

	s := stratum.NewStratum(&cfg)
	if cfg.Frontend.Enabled {
		go startFrontend(&cfg, s)
	}
	if cfg.Redis.Enabled {
		go NewRedisClient(&cfg)
	}
	s.Listen()
	return cfg.Address
}

func NewRedisClient(cfg *pool.Config) {
	redisAddr := fmt.Sprintf("%s:%v", cfg.Redis.Host, cfg.Redis.Port)
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,          // Defines redis addr, usually 127.0.0.1:6379
		Password: cfg.Redis.Password, // Generally pwd blank, option if required
		DB:       cfg.Redis.DB,       // Generally set to 0 for default DB
	})

	log.Printf("Redis DB Set To: %s => Index %v", redisAddr, cfg.Redis.DB)

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatal("[Fatal] Redis DB Connection Error: ", err.Error())
	}

	log.Printf("Redis DB Connection Successful: %s", redisAddr)
}

func startFrontend(cfg *pool.Config, s *stratum.StratumServer) {
	r := mux.NewRouter()
	r.HandleFunc("/stats", s.StatsIndex)
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./www/")))
	var err error
	if len(cfg.Frontend.Password) > 0 {
		auth := httpauth.SimpleBasicAuth(cfg.Frontend.Login, cfg.Frontend.Password)
		err = http.ListenAndServe(cfg.Frontend.Listen, auth(r))
	} else {
		err = http.ListenAndServe(cfg.Frontend.Listen, r)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func readConfig(cfg *pool.Config) {
	configFileName := "config.json"
	if len(os.Args) > 1 {
		configFileName = os.Args[1]
	}
	configFileName, _ = filepath.Abs(configFileName)
	log.Printf("Loading config: %v", configFileName)

	configFile, err := os.Open(configFileName)
	if err != nil {
		log.Fatal("File error: ", err.Error())
	}
	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	if err = jsonParser.Decode(&cfg); err != nil {
		log.Fatal("Config error: ", err.Error())
	}
}

func main() {
	rand.Seed(time.Now().UTC().UnixNano())
	readConfig(&cfg)
	startStratum()
}
