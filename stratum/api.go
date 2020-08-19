// Many api integration functions and ideas from: https://github.com/JKKGBE/open-zcash-pool which is a fork of https://github.com/sammy007/open-ethereum-pool
package stratum

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/util"
	"github.com/gorilla/mux"
)

type ApiServer struct {
	config              pool.APIConfig
	backend             *RedisClient
	hashrateWindow      time.Duration
	hashrateLargeWindow time.Duration
	stats               atomic.Value
	miners              map[string]*Entry
	minersMu            sync.RWMutex
	statsIntv           time.Duration
}

type Entry struct {
	stats     map[string]interface{}
	updatedAt int64
}

/*func StartAPI(cfg *pool.APIConfig, s *StratumServer) {
	r := mux.NewRouter()
	r.HandleFunc("/stats", s.StatsIndex)
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./www/")))
	var err error
	if len(cfg.Password) > 0 {
		auth := httpauth.SimpleBasicAuth(cfg.Login, cfg.Password)
		err = http.ListenAndServe(cfg.Listen, auth(r))
	} else {
		err = http.ListenAndServe(cfg.Listen, r)
	}
	if err != nil {
		log.Fatal(err)
	}
}*/

// StatsIndex pushes stats and surfaces to /stats page
/*func (s *StratumServer) StatsIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)

	hashrate, hashrate24h, totalOnline, miners := s.collectMinersStats()
	stats := map[string]interface{}{
		"miners":      miners,
		"hashrate":    hashrate,
		"hashrate24h": hashrate24h,
		"totalMiners": len(miners),
		"totalOnline": totalOnline,
		"timedOut":    len(miners) - totalOnline,
		"now":         util.MakeTimestamp(),
	}

	var upstreams []interface{}
	current := atomic.LoadInt32(&s.upstream)

	for i, u := range s.upstreams {
		upstream := convertUpstream(u)
		upstream["current"] = current == int32(i)
		upstreams = append(upstreams, upstream)
	}
	stats["upstreams"] = upstreams
	stats["current"] = convertUpstream(s.rpc())
	stats["luck"] = s.getLuckStats()
	stats["blocks"] = s.getBlocksStats()

	if t := s.currentBlockTemplate(); t != nil {
		stats["height"] = t.Height
		stats["diff"] = t.Difficulty
		roundShares := atomic.LoadInt64(&s.roundShares)
		stats["variance"] = float64(roundShares) / float64(t.Difficulty)
		stats["prevHash"] = t.Prev_Hash[0:8]
		stats["template"] = true
	}
	json.NewEncoder(w).Encode(stats)
}

func convertUpstream(u *rpc.RPCClient) map[string]interface{} {
	upstream := map[string]interface{}{
		"name":             u.Name,
		"url":              u.Url.String(),
		"sick":             u.Sick(),
		"accepts":          atomic.LoadInt64(&u.Accepts),
		"rejects":          atomic.LoadInt64(&u.Rejects),
		"lastSubmissionAt": atomic.LoadInt64(&u.LastSubmissionAt),
		"failsCount":       atomic.LoadInt64(&u.FailsCount),
		"info":             u.Info(),
	}
	return upstream
}

func (s *StratumServer) collectMinersStats() (float64, float64, int, []interface{}) {
	now := util.MakeTimestamp()
	var result []interface{}
	totalhashrate := float64(0)
	totalhashrate24h := float64(0)
	totalOnline := 0
	window24h := 24 * time.Hour

	for m := range s.miners.Iter() {
		stats := make(map[string]interface{})
		lastBeat := m.Val.getLastBeat()
		hashrate := m.Val.hashrate(s.estimationWindow)
		hashrate24h := m.Val.hashrate(window24h)
		totalhashrate += hashrate
		totalhashrate24h += hashrate24h
		stats["name"] = m.Key
		stats["hashrate"] = hashrate
		stats["hashrate24h"] = hashrate24h
		stats["lastBeat"] = lastBeat
		stats["validShares"] = atomic.LoadInt64(&m.Val.validShares)
		stats["staleShares"] = atomic.LoadInt64(&m.Val.staleShares)
		stats["invalidShares"] = atomic.LoadInt64(&m.Val.invalidShares)
		stats["accepts"] = atomic.LoadInt64(&m.Val.accepts)
		stats["rejects"] = atomic.LoadInt64(&m.Val.rejects)
		if !s.config.APIConfig.HideIP {
			stats["ip"] = m.Val.ip
		}

		if now-lastBeat > (int64(s.timeout/2) / 1000000) {
			stats["warning"] = true
		}
		if now-lastBeat > (int64(s.timeout) / 1000000) {
			stats["timeout"] = true
		} else {
			totalOnline++
		}
		result = append(result, stats)
	}
	return totalhashrate, totalhashrate24h, totalOnline, result
}

func (s *StratumServer) getLuckStats() map[string]interface{} {
	now := util.MakeTimestamp()
	var variance float64
	var totalVariance float64
	var blocksCount int
	var totalBlocksCount int

	s.blocksMu.Lock()
	defer s.blocksMu.Unlock()

	for k, v := range s.blockStats {
		if k >= now-int64(s.luckWindow) {
			blocksCount++
			variance += v.variance
		}
		if k >= now-int64(s.largeLuckWindow) {
			totalBlocksCount++
			totalVariance += v.variance
		} else {
			delete(s.blockStats, k)
		}
	}
	if blocksCount != 0 {
		variance = variance / float64(blocksCount)
	}
	if totalBlocksCount != 0 {
		totalVariance = totalVariance / float64(totalBlocksCount)
	}
	result := make(map[string]interface{})
	result["variance"] = variance
	result["blocksCount"] = blocksCount
	result["window"] = s.luckWindow
	result["totalVariance"] = totalVariance
	result["totalBlocksCount"] = totalBlocksCount
	result["largeWindow"] = s.largeLuckWindow
	return result
}

func (s *StratumServer) getBlocksStats() []interface{} {
	now := util.MakeTimestamp()
	var result []interface{}

	s.blocksMu.Lock()
	defer s.blocksMu.Unlock()

	for k, v := range s.blockStats {
		if k >= now-int64(s.largeLuckWindow) {
			block := map[string]interface{}{
				"height":    v.height,
				"hash":      v.hash,
				"variance":  v.variance,
				"timestamp": k,
			}
			result = append(result, block)
		} else {
			delete(s.blockStats, k)
		}
	}
	return result
}
*/

func NewApiServer(cfg *pool.APIConfig, s *StratumServer) *ApiServer {
	hashrateWindow, _ := time.ParseDuration(cfg.HashrateWindow)
	hashrateLargeWindow, _ := time.ParseDuration(cfg.HashrateLargeWindow)
	return &ApiServer{
		config:              *cfg,
		backend:             s.backend,
		hashrateWindow:      hashrateWindow,
		hashrateLargeWindow: hashrateLargeWindow,
		miners:              make(map[string]*Entry),
	}
}

func (apiServer *ApiServer) Start() {
	if apiServer.config.PurgeOnly {
		log.Printf("Starting API in purge-only mode")
	} else {
		log.Printf("Starting API on %v", apiServer.config.Listen)
	}

	apiServer.statsIntv, _ = time.ParseDuration(apiServer.config.StatsCollectInterval)
	statsTimer := time.NewTimer(apiServer.statsIntv)
	log.Printf("Set stats collect interval to %v", apiServer.statsIntv)

	purgeIntv, _ := time.ParseDuration(apiServer.config.PurgeInterval)
	purgeTimer := time.NewTimer(purgeIntv)
	log.Printf("Set purge interval to %v", purgeIntv)

	sort.Ints(apiServer.config.LuckWindow)

	if apiServer.config.PurgeOnly {
		apiServer.purgeStale()
	} else {
		apiServer.purgeStale()
		apiServer.collectStats()
	}

	go func() {
		for {
			select {
			case <-statsTimer.C:
				if !apiServer.config.PurgeOnly {
					apiServer.collectStats()
				}
				statsTimer.Reset(apiServer.statsIntv)
			case <-purgeTimer.C:
				apiServer.purgeStale()
				purgeTimer.Reset(purgeIntv)
			}
		}
	}()

	if !apiServer.config.PurgeOnly {
		apiServer.listen()
	}
}

func (apiServer *ApiServer) listen() {
	router := mux.NewRouter()
	router.HandleFunc("/api/stats", apiServer.StatsIndex)
	router.HandleFunc("/api/miners", apiServer.MinersIndex)
	router.HandleFunc("/api/blocks", apiServer.BlocksIndex)
	router.HandleFunc("/api/accounts/{login:t[0-9a-zA-Z]{34}}", apiServer.AccountIndex)
	router.NotFoundHandler = http.HandlerFunc(notFound)
	err := http.ListenAndServe(apiServer.config.Listen, router)
	if err != nil {
		log.Fatalf("Failed to start API: %v", err)
	}
}

func notFound(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusNotFound)
}

func (apiServer *ApiServer) purgeStale() {
	start := time.Now()
	total, err := apiServer.backend.FlushStaleStats(apiServer.hashrateWindow, apiServer.hashrateLargeWindow)
	if err != nil {
		log.Println("Failed to purge stale data from backend:", err)
	} else {
		log.Printf("Purged stale stats from backend, %v shares affected, elapsed time %v", total, time.Since(start))
	}
}

func (apiServer *ApiServer) collectStats() {
	start := time.Now()
	stats, err := apiServer.backend.CollectStats(apiServer.hashrateWindow, apiServer.config.Blocks)
	if err != nil {
		log.Printf("Failed to fetch stats from backend: %v", err)
		return
	}
	if len(apiServer.config.LuckWindow) > 0 {
		stats["luck"], err = apiServer.backend.CollectLuckStats(apiServer.config.LuckWindow)
		if err != nil {
			log.Printf("Failed to fetch luck stats from backend: %v", err)
			return
		}
	}
	apiServer.stats.Store(stats)
	log.Printf("Stats collection finished %s", time.Since(start))
}

func (apiServer *ApiServer) StatsIndex(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	nodes, err := apiServer.backend.GetNodeStates()
	if err != nil {
		log.Printf("Failed to get nodes stats from backend: %v", err)
	}
	reply["nodes"] = nodes

	stats := apiServer.getStats()
	if stats != nil {
		reply["now"] = util.MakeTimestamp() / 1000
		reply["stats"] = stats["stats"]
		reply["hashrate"] = stats["hashrate"]
		reply["minersTotal"] = stats["minersTotal"]
		reply["maturedTotal"] = stats["maturedTotal"]
		reply["immatureTotal"] = stats["immatureTotal"]
		reply["candidatesTotal"] = stats["candidatesTotal"]
	}

	err = json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (apiServer *ApiServer) MinersIndex(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	stats := apiServer.getStats()
	if stats != nil {
		reply["now"] = util.MakeTimestamp() / 1000
		reply["miners"] = stats["miners"]
		reply["hashrate"] = stats["hashrate"]
		reply["minersTotal"] = stats["minersTotal"]
	}

	err := json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (apiServer *ApiServer) BlocksIndex(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	stats := apiServer.getStats()
	if stats != nil {
		reply["matured"] = stats["matured"]
		reply["maturedTotal"] = stats["maturedTotal"]
		reply["immature"] = stats["immature"]
		reply["immatureTotal"] = stats["immatureTotal"]
		reply["candidates"] = stats["candidates"]
		reply["candidatesTotal"] = stats["candidatesTotal"]
		reply["luck"] = stats["luck"]
	}

	err := json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (apiServer *ApiServer) AccountIndex(writer http.ResponseWriter, r *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")

	login := mux.Vars(r)["login"]
	apiServer.minersMu.Lock()
	defer apiServer.minersMu.Unlock()

	reply, ok := apiServer.miners[login]
	now := util.MakeTimestamp() / 1000
	cacheIntv := int64(apiServer.statsIntv / time.Millisecond)
	// Refresh stats if stale
	if !ok || reply.updatedAt < now-cacheIntv {
		exist, err := apiServer.backend.IsMinerExists(login)
		if !exist {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}

		stats, err := apiServer.backend.GetMinerStats(login)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}
		workers, err := apiServer.backend.CollectWorkersStats(apiServer.hashrateWindow, apiServer.hashrateLargeWindow, login)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}
		for key, value := range workers {
			stats[key] = value
		}

		reply = &Entry{stats: stats, updatedAt: now}
		apiServer.miners[login] = reply
	}

	writer.WriteHeader(http.StatusOK)
	err := json.NewEncoder(writer).Encode(reply.stats)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (apiServer *ApiServer) getStats() map[string]interface{} {
	stats := apiServer.stats.Load()
	if stats != nil {
		return stats.(map[string]interface{})
	}
	return nil
}
