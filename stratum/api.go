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
	stratum             *StratumServer
}

type Entry struct {
	stats     map[string]interface{}
	updatedAt int64
}

func NewApiServer(cfg *pool.APIConfig, s *StratumServer) *ApiServer {
	hashrateWindow, _ := time.ParseDuration(cfg.HashrateWindow)
	hashrateLargeWindow, _ := time.ParseDuration(cfg.HashrateLargeWindow)
	return &ApiServer{
		config:              *cfg,
		backend:             s.backend,
		hashrateWindow:      hashrateWindow,
		hashrateLargeWindow: hashrateLargeWindow,
		miners:              make(map[string]*Entry),
		stratum:             s,
	}
}

func (apiServer *ApiServer) Start() {
	if apiServer.config.PurgeOnly {
		log.Printf("[API] Starting API in purge-only mode")
	} else {
		log.Printf("[API] Starting API on %v", apiServer.config.Listen)
	}

	apiServer.statsIntv, _ = time.ParseDuration(apiServer.config.StatsCollectInterval)
	statsTimer := time.NewTimer(apiServer.statsIntv)
	log.Printf("[API] Set stats collect interval to %v", apiServer.statsIntv)

	purgeIntv, _ := time.ParseDuration(apiServer.config.PurgeInterval)
	purgeTimer := time.NewTimer(purgeIntv)
	log.Printf("[API] Set purge interval to %v", purgeIntv)

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
	router.HandleFunc("/api/payments", apiServer.PaymentsIndex)
	router.HandleFunc("/api/allstats", apiServer.AllStatsIndex)
	router.HandleFunc("/api/health", apiServer.GetHealthIndex)
	router.HandleFunc("/api/accounts/{login:dE[0-9a-zA-Z]{96}}", apiServer.AccountIndex)
	router.NotFoundHandler = http.HandlerFunc(notFound)
	err := http.ListenAndServe(apiServer.config.Listen, router)
	if err != nil {
		log.Fatalf("[API] Failed to start API: %v", err)
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
		log.Println("[API] Failed to purge stale data from backend:", err)
	} else {
		log.Printf("[API] Purged stale stats from backend, %v shares affected, elapsed time %v", total, time.Since(start))
	}
}

func (apiServer *ApiServer) collectStats() {
	//start := time.Now()
	stats, err := apiServer.backend.CollectStats(apiServer.hashrateWindow, apiServer.config.Blocks, apiServer.config.Payments)
	if err != nil {
		log.Printf("[API] Failed to fetch stats from backend: %v", err)
		return
	}
	if len(apiServer.config.LuckWindow) > 0 {
		stats["luck"], err = apiServer.backend.CollectLuckStats(apiServer.config.LuckWindow)
		if err != nil {
			log.Printf("[API] Failed to fetch luck stats from backend: %v", err)
			return
		}
	}
	apiServer.stats.Store(stats)
	//log.Printf("Stats collection finished %s", time.Since(start))
}

func (apiServer *ApiServer) AllStatsIndex(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	nodes, err := apiServer.backend.GetNodeStates()
	if err != nil {
		log.Printf("[API] Failed to get nodes stats from backend: %v", err)
	}
	reply["nodes"] = nodes

	network, err := apiServer.backend.GetLastNetworkStates()
	if err != nil {
		log.Printf("[API] Failed to get network stats from backend: %v", err)
	}
	reply["network"] = network

	lastblock, err := apiServer.backend.GetLastBlockStates()
	if err != nil {
		log.Printf("[API] Failed to get lastblock stats from backend: %v", err)
	}
	reply["lastblock"] = lastblock

	stats := apiServer.getStats()
	if stats != nil {

		reply["blocks"] = map[string]interface{}{"totalBlocks": stats["totalBlocks"], "totalBlocksSolo": stats["totalBlocksSolo"], "maturedTotal": stats["maturedTotal"], "maturedTotalSolo": stats["maturedTotalSolo"], "immatureTotal": stats["immatureTotal"], "immatureTotalSolo": stats["immatureTotalSolo"], "candidatesTotal": stats["candidatesTotal"], "luck": stats["luck"], "matured": stats["matured"], "maturedSolo": stats["maturedSolo"], "immature": stats["immature"], "immatureSolo": stats["immatureSolo"], "candidates": stats["candidates"]}
		reply["payments"] = map[string]interface{}{"totalMinersPaid": stats["totalMinersPaid"], "paymentsTotal": stats["paymentsTotal"], "payments": stats["payments"]}
		reply["miners"] = map[string]interface{}{"hashrate": stats["hashrate"], "hashrateSolo": stats["hashrateSolo"], "minersTotal": stats["minersTotal"], "minersTotalSolo": stats["minersTotalSolo"], "miners": stats["miners"], "minersSolo": stats["minersSolo"]}
		reply["now"] = util.MakeTimestamp() / 1000
		reply["stats"] = map[string]interface{}{"poolstats": stats["stats"], "hashrate": stats["hashrate"], "hashrateSolo": stats["hashrateSolo"], "minersTotal": stats["minersTotal"]}
		reply["config"] = apiServer.GetConfigIndex()
		reply["charts"] = map[string]interface{}{}
	}

	err = json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Println("[API] Error serializing API response: ", err)
	}
}

func (apiServer *ApiServer) GetConfigIndex() map[string]interface{} {
	stats := make(map[string]interface{})

	stats["poolHost"] = apiServer.stratum.config.PoolHost
	stats["blockchainExplorer"] = apiServer.stratum.config.BlockchainExplorer
	stats["transactionExplorer"] = apiServer.stratum.config.TransactionExploer
	stats["version"] = "1.0.0"
	stats["algo"] = apiServer.stratum.config.Algo
	stats["coin"] = apiServer.stratum.config.Coin
	stats["coinUnits"] = apiServer.stratum.config.CoinUnits
	stats["coinDecimalPlaces"] = apiServer.stratum.config.CoinDecimalPlaces
	stats["coinDifficultyTarget"] = apiServer.stratum.config.CoinDifficultyTarget
	stats["payIDAddressSeparator"] = apiServer.stratum.config.Stratum.PaymentID.AddressSeparator
	stats["workIDAddressSeparator"] = apiServer.stratum.config.Stratum.WorkerID.AddressSeparator
	stats["fixedDiffAddressSeparator"] = apiServer.stratum.config.Stratum.FixedDiff.AddressSeparator
	stats["ports"] = apiServer.stratum.config.Stratum.Ports
	stats["unlockDepth"] = apiServer.stratum.config.UnlockerConfig.Depth
	unlockTime, _ := time.ParseDuration(apiServer.stratum.config.UnlockerConfig.Interval)
	unlockInterval := int64(unlockTime / time.Second)
	stats["unlockInterval"] = unlockInterval
	stats["poolFee"] = apiServer.stratum.config.UnlockerConfig.PoolFee
	stats["paymentMixin"] = apiServer.stratum.config.PaymentsConfig.Mixin
	stats["paymentMinimum"] = apiServer.stratum.config.PaymentsConfig.Threshold
	paymentTime, _ := time.ParseDuration(apiServer.stratum.config.PaymentsConfig.Interval)
	paymentInterval := int64(paymentTime / time.Second)
	stats["paymentInterval"] = paymentInterval

	return stats
}

func (apiServer *ApiServer) GetHealthIndex(writer http.ResponseWriter, _ *http.Request) {
	reply := make(map[string]interface{})

	r := apiServer.stratum.rpc()
	info, err := r.GetInfo()
	if err != nil {
		reply["health"] = ""
		log.Printf("[API] Unable to get current blockchain height from node: %v", err)
	} else {
		reply["health"] = "OK"
	}

	reply["network"] = map[string]interface{}{"difficulty": info.Difficulty, "topoheight": info.Topoheight, "height": info.Height, "stableheight": info.Stableheight, "averageblocktime50": info.Averageblocktime50, "testnet": info.Testnet, "target": info.Target, "topblockhash": info.TopBlockHash, "txpoolsize": info.TxPoolSize, "totalsupply": info.TotalSupply, "version": info.Version}

	err = json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Println("[API] Error serializing API response: ", err)
	}
}

func (apiServer *ApiServer) StatsIndex(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	nodes, err := apiServer.backend.GetNodeStates()
	if err != nil {
		log.Printf("[API] Failed to get nodes stats from backend: %v", err)
	}
	reply["nodes"] = nodes

	stats := apiServer.getStats()
	if stats != nil {
		reply["now"] = util.MakeTimestamp() / 1000
		reply["stats"] = stats["stats"]
		reply["hashrate"] = stats["hashrate"]
		reply["hashrateSolo"] = stats["hashrateSolo"]
		reply["minersTotal"] = stats["minersTotal"]
		reply["maturedTotal"] = stats["maturedTotal"]
		reply["immatureTotal"] = stats["immatureTotal"]
		reply["candidatesTotal"] = stats["candidatesTotal"]
	}

	err = json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Println("[API] Error serializing API response: ", err)
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
		log.Println("[API] Error serializing API response: ", err)
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
		log.Println("[API] Error serializing API response: ", err)
	}
}

func (s *ApiServer) PaymentsIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	stats := s.getStats()
	if stats != nil {
		reply["payments"] = stats["payments"]
		reply["paymentsTotal"] = stats["paymentsTotal"]
	}

	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("[API] Error serializing API response: ", err)
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
			log.Printf("[API] Failed to fetch stats from backend: %v", err)
			return
		}

		stats, err := apiServer.backend.GetMinerStats(login, apiServer.config.Payments)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			log.Printf("[API] Failed to fetch stats from backend: %v", err)
			return
		}
		workers, err := apiServer.backend.CollectWorkersStats(apiServer.hashrateWindow, apiServer.hashrateLargeWindow, login)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			log.Printf("[API] Failed to fetch stats from backend: %v", err)
			return
		}
		for key, value := range workers {
			stats[key] = value
		}
		stats["pageSize"] = apiServer.config.Payments
		reply = &Entry{stats: stats, updatedAt: now}
		apiServer.miners[login] = reply
	}

	writer.WriteHeader(http.StatusOK)
	err := json.NewEncoder(writer).Encode(reply.stats)
	if err != nil {
		log.Println("[API] Error serializing API response: ", err)
	}
}

func (apiServer *ApiServer) getStats() map[string]interface{} {
	stats := apiServer.stats.Load()
	if stats != nil {
		return stats.(map[string]interface{})
	}
	return nil
}
