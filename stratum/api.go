// Many api integration functions and ideas from: https://github.com/JKKGBE/open-zcash-pool which is a fork of https://github.com/sammy007/open-ethereum-pool
package stratum

import (
	"encoding/json"
	"log"
	"net/http"
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
	Hash      string
	Timestamp int64
	Payees    uint64
	Mixin     uint64
	Amount    uint64
}

type ApiMiner struct {
	LastBeat        int64
	StartedAt       int64
	ValidShares     int64
	InvalidShares   int64
	StaleShares     int64
	Accepts         int64
	Rejects         int64
	LastRoundShares int64
	RoundShares     int64
	Hashrate        int64
	Offline         bool
	sync.RWMutex
	Id      string
	Address string
	IsSolo  bool
}

type ApiBlocks struct {
	Hash        string
	Address     string
	Height      int64
	Orphan      bool
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

	log.Printf("[API] Starting API on %v", apiServer.config.Listen)

	apiServer.statsIntv, _ = time.ParseDuration(apiServer.config.StatsCollectInterval)
	statsTimer := time.NewTimer(apiServer.statsIntv)
	log.Printf("[API] Set stats collect interval to %v", apiServer.statsIntv)

	apiServer.collectStats()

	go func() {
		for {
			select {
			case <-statsTimer.C:
				apiServer.collectStats()
				statsTimer.Reset(apiServer.statsIntv)
			}
		}
	}()

	apiServer.listen()
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

func (apiServer *ApiServer) collectStats() {
	//start := time.Now()
	stats := make(map[string]interface{})
	var numCandidateBlocks, numImmatureBlocks, numMaturedBlocks int

	// Build last block stats
	stats["lastblock"] = apiServer.backend.GetLastBlock()

	// Build Payments stats
	processedPayments := apiServer.backend.GetProcessedPayments()
	if processedPayments != nil {
		apiPayments, totalPayments, totalMinersPaid := apiServer.convertPaymentsResults(processedPayments)
		stats["payments"] = apiPayments
		stats["totalPayments"] = totalPayments
		stats["totalMinersPaid"] = totalMinersPaid
	}

	// Build found block stats
	candidateBlocks := apiServer.backend.GetBlocksFound("candidate")
	if candidateBlocks != nil {
		apiCandidates := apiServer.convertBlocksResults(candidateBlocks.MinedBlocks)
		stats["candidates"] = apiCandidates
		numCandidateBlocks = len(candidateBlocks.MinedBlocks)
	}

	immatureBlocks := apiServer.backend.GetBlocksFound("immature")
	if immatureBlocks != nil {
		apiImmature := apiServer.convertBlocksResults(immatureBlocks.MinedBlocks)
		stats["immature"] = apiImmature
		numImmatureBlocks = len(immatureBlocks.MinedBlocks)
	}

	maturedBlocks := apiServer.backend.GetBlocksFound("matured")
	if maturedBlocks != nil {
		apiMatured := apiServer.convertBlocksResults(maturedBlocks.MinedBlocks)
		stats["matured"] = apiMatured
		numMaturedBlocks = len(maturedBlocks.MinedBlocks)
	}

	stats["candidatesTotal"] = numCandidateBlocks
	stats["immatureTotal"] = numImmatureBlocks
	stats["maturedTotal"] = numMaturedBlocks
	stats["blocksTotal"] = numCandidateBlocks + numImmatureBlocks + numMaturedBlocks

	// Build miner stats
	minerStats := apiServer.backend.GetAllMinerStats()
	apiMiners, poolHashrate, totalPoolMiners, soloHashrate, totalSoloMiners := apiServer.convertMinerResults(minerStats)
	stats["miners"] = apiMiners
	stats["poolHashrate"] = poolHashrate
	stats["totalPoolMiners"] = totalPoolMiners
	stats["soloHashrate"] = soloHashrate
	stats["totalSoloMiners"] = totalSoloMiners
	apiServer.stats.Store(stats)
	//log.Printf("Stats collection finished %s", time.Since(start))
}

func (apiServer *ApiServer) convertPaymentsResults(processedPayments *ProcessedPayments) ([]*ApiPayments, int64, int64) {
	apiPayments := make(map[string]*ApiPayments)
	var paymentsArr []*ApiPayments
	var totalPayments int64
	var totalMinersPaid int
	var tempMinerArr []string

	for _, value := range processedPayments.MinerPayments {
		reply := &ApiPayments{}

		// Check through for duplicate addresses to populate totalMinersPaid
		var mExist bool
		for _, m := range tempMinerArr {
			if value.Login == m {
				mExist = true
			}
		}
		if !mExist {
			tempMinerArr = append(tempMinerArr, value.Login)
		}

		// Check to ensure apiPayments has items
		if len(apiPayments) > 0 {
			// Check to ensure value.TxHash exists within apiPayments
			v, found := apiPayments[value.TxHash]
			if found {
				// Append details such as amount, payees, etc.
				reply = apiPayments[value.TxHash]
				reply.Amount = v.Amount + value.Amount
				reply.Payees = v.Payees + 1
				reply.Mixin = value.Mixin
			} else {
				reply = &ApiPayments{Hash: value.TxHash, Timestamp: value.Timestamp, Mixin: value.Mixin, Amount: value.Amount, Payees: 1}
				totalPayments++
			}
		} else {
			reply = &ApiPayments{Hash: value.TxHash, Timestamp: value.Timestamp, Mixin: value.Mixin, Amount: value.Amount, Payees: 1}
			totalPayments++
		}
		apiPayments[value.TxHash] = reply
	}
	totalMinersPaid = len(tempMinerArr)

	for p := range apiPayments {
		paymentsArr = append(paymentsArr, apiPayments[p])
	}

	return paymentsArr, totalPayments, int64(totalMinersPaid)
}

func (apiServer *ApiServer) convertBlocksResults(minedBlocks []*BlockDataGrav) []*ApiBlocks {
	apiBlocks := make(map[string]*ApiBlocks)
	var blocksArr []*ApiBlocks
	for _, value := range minedBlocks {
		reply := &ApiBlocks{}
		trimmedAddr := value.Address[0:7] + "..." + value.Address[len(value.Address)-5:len(value.Address)]
		// Check to ensure apiBlocks has items
		reply = &ApiBlocks{Hash: value.Hash, Address: trimmedAddr, Height: value.Height, Orphan: value.Orphan, Timestamp: value.Timestamp, Difficulty: value.Difficulty, TotalShares: value.TotalShares, Reward: value.Reward, Solo: value.Solo}
		apiBlocks[value.Hash] = reply
	}
	for b := range apiBlocks {
		blocksArr = append(blocksArr, apiBlocks[b])
	}

	return blocksArr
}

func (apiServer *ApiServer) convertMinerResults(miners []*Miner) ([]*ApiMiner, int64, int64, int64, int64) {
	//registeredMiners := apiServer.backend.GetMinerIDRegistrations()
	apiMiners := make(map[string]*ApiMiner)
	var minersArr []*ApiMiner
	var poolHashrate int64
	var soloHashrate int64
	var totalPoolMiners int64
	var totalSoloMiners int64

	for _, currMiner := range miners {
		reply := &ApiMiner{}
		if miners != nil {
			//currMiner, _ := miners.Get(value.Id)
			if currMiner != nil {
				var tempDuration time.Duration
				now := util.MakeTimestamp() / 1000
				var windowHashes bool
				// If hashrateExpiration is set to -1, then keep data forever so no need to filter out old data
				if apiServer.stratum.hashrateExpiration == tempDuration {
					windowHashes = true
				} else {
					maxLastBeat := now - int64(apiServer.stratum.hashrateExpiration/time.Second)
					windowHashes = currMiner.LastBeat >= maxLastBeat
				}
				if currMiner != nil && windowHashes {
					var Offline bool
					var Hashrate int64
					var ID string

					// Set miner to offline
					if currMiner.LastBeat < (now - int64(apiServer.stratum.estimationWindow/time.Second)/2) {
						Offline = true
					}

					// Get miner hashrate (updates api side for accurate representation, even if miner has been offline)
					if !Offline {
						Hashrate = currMiner.getHashrate(apiServer.stratum.estimationWindow, apiServer.stratum.hashrateExpiration)
					}

					// Utilizing extracted workid value, could be leveraging workid instead of full id value for stats [later worker stats on a per-address layer potentially]
					if currMiner.WorkID != "" {
						ID = currMiner.WorkID
					} else {
						ID = currMiner.Id
					}

					// Generate struct for miner stats
					reply = &ApiMiner{
						LastBeat:        currMiner.LastBeat,
						StartedAt:       currMiner.StartedAt,
						ValidShares:     currMiner.ValidShares,
						InvalidShares:   currMiner.InvalidShares,
						StaleShares:     currMiner.StaleShares,
						Accepts:         currMiner.Accepts,
						Rejects:         currMiner.Rejects,
						LastRoundShares: currMiner.LastRoundShares,
						RoundShares:     currMiner.RoundShares,
						Hashrate:        Hashrate,
						Offline:         Offline,
						Id:              ID,
						Address:         currMiner.Address,
						IsSolo:          currMiner.IsSolo,
					}

					apiMiners[ID] = reply

					// Compound pool stats: solo hashrate/miners and pool hashrate/miners
					if currMiner.IsSolo && !Offline {
						soloHashrate += Hashrate
						totalSoloMiners++
					} else if !Offline {
						poolHashrate += Hashrate
						totalPoolMiners++
					}
				}
			}
		}
	}
	for m := range apiMiners {
		minersArr = append(minersArr, apiMiners[m])
	}

	return minersArr, poolHashrate, totalPoolMiners, soloHashrate, totalSoloMiners
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

func (apiServer *ApiServer) StatsIndex(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})

	stats := apiServer.getStats()
	if stats != nil {
		reply["now"] = util.MakeTimestamp() / 1000
		reply["lastblock"] = stats["lastblock"]
		reply["config"] = apiServer.GetConfigIndex()
		reply["payments"] = stats["payments"]
		reply["totalPayments"] = stats["totalPayments"]
		reply["totalMinersPaid"] = stats["totalMinersPaid"]
		reply["candidates"] = stats["candidates"]
		reply["immature"] = stats["immature"]
		reply["matured"] = stats["matured"]
		reply["candidatesTotal"] = stats["candidatesTotal"]
		reply["immatureTotal"] = stats["immatureTotal"]
		reply["maturedTotal"] = stats["maturedTotal"]
		reply["blocksTotal"] = stats["blocksTotal"]
		reply["miners"] = stats["miners"]
		reply["poolHashrate"] = stats["poolHashrate"]
		reply["totalPoolMiners"] = stats["totalPoolMiners"]
		reply["soloHashrate"] = stats["soloHashrate"]
		reply["totalSoloMiners"] = stats["totalSoloMiners"]
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
