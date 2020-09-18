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
	backend             *GravitonStore
	hashrateWindow      time.Duration
	hashrateLargeWindow time.Duration
	stats               atomic.Value
	miners              map[string]*Entry
	minersMu            sync.RWMutex
	statsIntv           time.Duration
	stratum             *StratumServer
}

type ApiPayments struct {
	Payees uint64
	Mixin  uint64
	Amount uint64
}

type ApiBlocks struct {
	Address     string
	Height      int64
	Orphan      bool
	Nonce       string
	Timestamp   int64
	Difficulty  int64
	TotalShares int64
	Reward      uint64
	Solo        bool
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
		backend:             s.gravitonDB,
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
		log.Printf("[API] Would be purging stale...")
		//apiServer.purgeStale()
	} else {
		log.Printf("[API] Would be purging stale...")
		//apiServer.purgeStale()
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
				log.Printf("[API] Would be purging stale...")
				//apiServer.purgeStale()
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
	//router.HandleFunc("/api/accounts/{login:dE[0-9a-zA-Z]{96}}", apiServer.AccountIndex)
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

/*
func (apiServer *ApiServer) purgeStale() {
	start := time.Now()
	total, err := apiServer.backend.FlushStaleStats(apiServer.hashrateWindow, apiServer.hashrateLargeWindow)
	if err != nil {
		log.Println("[API] Failed to purge stale data from backend:", err)
	} else {
		log.Printf("[API] Purged stale stats from backend, %v shares affected, elapsed time %v", total, time.Since(start))
	}
}
*/

func (apiServer *ApiServer) collectStats() {
	//start := time.Now()
	stats := make(map[string]interface{})

	/*
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
	*/

	// Build last block stats
	stats["lastblock"] = apiServer.backend.GetLastBlock()

	// Build Payments stats
	processedPayments := apiServer.backend.GetProcessedPayments()
	apiPayments := apiServer.convertPaymentsResults(processedPayments)
	stats["payments"] = apiPayments

	// Build found block stats
	candidateBlocks := apiServer.backend.GetBlocksFound("candidate")
	apiCandidates := apiServer.convertBlocksResults(candidateBlocks.MinedBlocks)
	stats["candidates"] = apiCandidates
	stats["candidatesTotal"] = len(candidateBlocks.MinedBlocks)
	immatureBlocks := apiServer.backend.GetBlocksFound("immature")
	apiImmature := apiServer.convertBlocksResults(immatureBlocks.MinedBlocks)
	stats["immature"] = apiImmature
	stats["immatureTotal"] = len(immatureBlocks.MinedBlocks)
	maturedBlocks := apiServer.backend.GetBlocksFound("matured")
	apiMatured := apiServer.convertBlocksResults(maturedBlocks.MinedBlocks)
	stats["matured"] = apiMatured
	stats["maturedTotal"] = len(maturedBlocks.MinedBlocks)
	stats["blocksTotal"] = len(candidateBlocks.MinedBlocks) + len(immatureBlocks.MinedBlocks) + len(maturedBlocks.MinedBlocks)

	// Build miner stats
	minerStats := apiServer.backend.GetAllMinerStats()
	apiMiners := apiServer.convertMinerResults(minerStats)
	stats["miners"] = apiMiners
	apiServer.stats.Store(stats)
	//log.Printf("Stats collection finished %s", time.Since(start))
}

func (apiServer *ApiServer) convertPaymentsResults(processedPayments *ProcessedPayments) map[string]*ApiPayments {
	apiPayments := make(map[string]*ApiPayments)
	for _, value := range processedPayments.MinerPayments {
		reply := &ApiPayments{}
		// Check to ensure apiPayments has items
		if len(apiPayments) > 0 {
			// Check to ensure value.TxHash exists within apiPayments
			v, found := apiPayments[value.TxHash]
			if found {
				// Append details such as amount, payees, etc.
				reply.Amount = v.Amount + value.Amount
				reply.Payees = v.Payees + 1
				reply.Mixin = value.Mixin
			} else {
				reply = &ApiPayments{Mixin: value.Mixin, Amount: value.Amount, Payees: 1}
			}
		} else {
			reply = &ApiPayments{Mixin: value.Mixin, Amount: value.Amount, Payees: 1}
		}
		apiPayments[value.TxHash] = reply
	}
	return apiPayments
}

func (apiServer *ApiServer) convertBlocksResults(minedBlocks []*BlockDataGrav) map[string]*ApiBlocks {
	apiBlocks := make(map[string]*ApiBlocks)
	for _, value := range minedBlocks {
		reply := &ApiBlocks{}
		// Check to ensure apiBlocks has items
		reply = &ApiBlocks{Address: value.Address, Height: value.Height, Orphan: value.Orphan, Nonce: value.Nonce, Timestamp: value.Timestamp, Difficulty: value.Difficulty, TotalShares: value.TotalShares, Reward: value.Reward, Solo: value.Solo}
		apiBlocks[value.Hash] = reply
	}
	return apiBlocks
}

func (apiServer *ApiServer) convertMinerResults(miners *MinersMap) map[string]*Miner {
	registeredMiners := apiServer.backend.GetMinerIDRegistrations()
	apiMiners := make(map[string]*Miner)

	for _, value := range registeredMiners {
		currMiner, _ := miners.Get(value.Id)
		if currMiner != nil {
			var tempDuration time.Duration
			now := util.MakeTimestamp() / 1000
			var windowHashes bool
			// If hashrateExpiration is set to -1, then keep data forever so no need to filter out old data
			if apiServer.stratum.hashrateExpiration == tempDuration {
				windowHashes = true
			} else {
				maxLastBeat := now - int64(apiServer.stratum.hashrateExpiration/time.Second)
				windowHashes = (currMiner.LastBeat / 1000) >= maxLastBeat
			}
			if currMiner != nil && windowHashes {
				apiMiners[currMiner.Id] = currMiner
			}
		}
	}
	return apiMiners
}

// Try to convert all numeric strings to int64
/*
func (apiServer *ApiServer) convertStringMap(m map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	var err error
	for k, v := range m {
		result[k], err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			result[k] = v
		}
	}
	return result
}
*/

/*
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
*/

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

func (apiServer *ApiServer) StatsIndex(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	/*
		nodes, err := apiServer.backend.GetNodeStates()
		if err != nil {
			log.Printf("[API] Failed to get nodes stats from backend: %v", err)
		}
		reply["nodes"] = nodes
	*/

	stats := apiServer.getStats()
	if stats != nil {
		reply["now"] = util.MakeTimestamp() / 1000
		reply["lastblock"] = stats["lastblock"]
		reply["config"] = apiServer.GetConfigIndex()
		reply["payments"] = stats["payments"]
		reply["candidates"] = stats["candidates"]
		reply["immature"] = stats["immature"]
		reply["matured"] = stats["matured"]
		reply["candidatesTotal"] = stats["candidatesTotal"]
		reply["immatureTotal"] = stats["immatureTotal"]
		reply["maturedTotal"] = stats["maturedTotal"]
		reply["blocksTotal"] = stats["blocksTotal"]
		reply["miners"] = stats["miners"]
		//reply["stats"] = stats["stats"]
		//reply["hashrate"] = stats["hashrate"]
		//reply["hashrateSolo"] = stats["hashrateSolo"]
		//reply["minersTotal"] = stats["minersTotal"]
		//reply["maturedTotal"] = stats["maturedTotal"]
		//reply["immatureTotal"] = stats["immatureTotal"]
		//reply["candidatesTotal"] = stats["candidatesTotal"]
	}

	err := json.NewEncoder(writer).Encode(reply)
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
