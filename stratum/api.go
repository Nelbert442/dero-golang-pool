// Many api integration functions and ideas from: https://github.com/JKKGBE/open-zcash-pool which is a fork of https://github.com/sammy007/open-ethereum-pool
package stratum

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Nelbert442/dero-golang-pool/pool"
	"github.com/Nelbert442/dero-golang-pool/util"

	"github.com/gorilla/mux"
)

type ApiServer struct {
	config         pool.APIConfig
	eventsconfig   pool.EventsConfig
	backend        *GravitonStore
	hashrateWindow time.Duration
	stats          atomic.Value
	//miners         map[string]*Entry
	//minersMu       sync.RWMutex
	statsIntv time.Duration
	stratum   *StratumServer
}

type ApiPayments struct {
	Hash      string
	Timestamp int64
	Payees    uint64
	Mixin     uint64
	Amount    uint64
	Fee       uint64
}

type ApiEventPayments struct {
	Timestamp int64
	Amount    uint64
	Address   string
}

type ApiMiner struct {
	LastBeat      int64
	StartedAt     int64
	ValidShares   int64
	InvalidShares int64
	StaleShares   int64
	Accepts       int64
	Rejects       int64
	RoundShares   int64
	Hashrate      int64
	Offline       bool
	sync.RWMutex
	Id            string
	Address       string
	IsSolo        bool
	DonatePercent int64
	DonationTotal int64
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

type LastBlock struct {
	Difficulty string
	Height     int64
	Timestamp  int64
	Reward     int64
	Hash       string
}

/*
type Entry struct {
	stats     map[string]interface{}
	updatedAt int64
}
*/

var APIInfoLogger = logFileOutAPI("INFO")
var APIErrorLogger = logFileOutAPI("ERROR")

func NewApiServer(cfg *pool.APIConfig, s *StratumServer, eventsconfig *pool.EventsConfig) *ApiServer {
	hashrateWindow, _ := time.ParseDuration(cfg.HashrateWindow)
	return &ApiServer{
		config:         *cfg,
		eventsconfig:   *eventsconfig,
		backend:        Graviton_backend,
		hashrateWindow: hashrateWindow,
		//miners:         make(map[string]*Entry),
		stratum: s,
	}
}

func (apiServer *ApiServer) Start() {

	apiServer.statsIntv, _ = time.ParseDuration(apiServer.config.StatsCollectInterval)
	statsTimer := time.NewTimer(apiServer.statsIntv)
	log.Printf("[API] Set stats collect interval to %v", apiServer.statsIntv)
	APIInfoLogger.Printf("[API] Set stats collect interval to %v", apiServer.statsIntv)

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

	// If SSL is configured, due to nature of listenandserve, put HTTP in go routine then call SSL afterwards so they can run in parallel. Otherwise, run http as normal
	if apiServer.config.SSL {
		go apiServer.listen()
		apiServer.listenSSL()
	} else {
		apiServer.listen()
	}
}

func (apiServer *ApiServer) listen() {
	log.Printf("[API] Starting API on %v", apiServer.config.Listen)
	APIInfoLogger.Printf("[API] Starting API on %v", apiServer.config.Listen)
	router := mux.NewRouter()
	router.HandleFunc("/api/stats", apiServer.StatsIndex)
	router.HandleFunc("/api/blocks", apiServer.BlocksIndex)
	router.HandleFunc("/api/payments", apiServer.PaymentsIndex)
	router.HandleFunc("/api/miners", apiServer.MinersIndex)
	router.HandleFunc("/api/accounts", apiServer.AccountIndex)
	router.HandleFunc("/api/charts", apiServer.ChartsIndex)
	router.HandleFunc("/api/events", apiServer.EventsIndex)
	router.NotFoundHandler = http.HandlerFunc(notFound)
	err := http.ListenAndServe(apiServer.config.Listen, router)
	if err != nil {
		APIErrorLogger.Printf("[API] Failed to start API: %v", err)
		log.Fatalf("[API] Failed to start API: %v", err)
	}
}

func (apiServer *ApiServer) listenSSL() {
	log.Printf("[API] Starting SSL API on %v", apiServer.config.SSLListen)
	APIInfoLogger.Printf("[API] Starting SSL API on %v", apiServer.config.SSLListen)
	routerSSL := mux.NewRouter()
	routerSSL.HandleFunc("/api/stats", apiServer.StatsIndex)
	routerSSL.HandleFunc("/api/blocks", apiServer.BlocksIndex)
	routerSSL.HandleFunc("/api/payments", apiServer.PaymentsIndex)
	routerSSL.HandleFunc("/api/miners", apiServer.MinersIndex)
	routerSSL.HandleFunc("/api/accounts", apiServer.AccountIndex)
	routerSSL.HandleFunc("/api/charts", apiServer.ChartsIndex)
	routerSSL.HandleFunc("/api/events", apiServer.EventsIndex)
	routerSSL.NotFoundHandler = http.HandlerFunc(notFound)
	err := http.ListenAndServeTLS(apiServer.config.SSLListen, apiServer.config.CertFile, apiServer.config.KeyFile, routerSSL)
	if err != nil {
		APIErrorLogger.Printf("[API] Failed to start SSL API: %v", err)
		log.Fatalf("[API] Failed to start SSL API: %v", err)
	}
}

func notFound(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusNotFound)
}

func (apiServer *ApiServer) collectStats() {
	stats := make(map[string]interface{})
	var numCandidateBlocks, numImmatureBlocks, numMaturedBlocks int

	// Build lastblock stats
	v := apiServer.stratum.rpc()
	prevBlock, getHashERR := v.GetLastBlockHeader()

	if getHashERR != nil {
		log.Printf("[API] Error while retrieving block from node: %v", getHashERR)
		APIErrorLogger.Printf("[API] Error while retrieving block from node: %v", getHashERR)
		lastblockDB := &LastBlock{}
		stats["lastblock"] = lastblockDB
	} else {
		lastBlock := prevBlock.BlockHeader
		lastblockDB := &LastBlock{Difficulty: lastBlock.Difficulty, Height: lastBlock.Height, Timestamp: int64(lastBlock.Timestamp), Reward: int64(lastBlock.Reward), Hash: lastBlock.Hash}
		stats["lastblock"] = lastblockDB
	}

	// Build Payments stats
	processedPayments := apiServer.backend.GetProcessedPayments()
	if processedPayments != nil {
		apiPayments, totalPayments, totalMinersPaid := apiServer.convertPaymentsResults(processedPayments)
		if int64(len(apiPayments)) > apiServer.config.Payments {
			stats["paymentsSmall"] = apiPayments[0:apiServer.config.Payments] // Only return the last x number of payments, defined by payments within config.json
		} else {
			stats["paymentsSmall"] = apiPayments
		}
		stats["payments"] = apiPayments // Retain full list for other use cases in load more options etc.
		stats["totalPayments"] = totalPayments
		stats["totalMinersPaid"] = totalMinersPaid
	}

	// Build found block stats
	candidateBlocks := apiServer.backend.GetBlocksFound("candidate")
	if candidateBlocks != nil {
		apiCandidates := apiServer.convertBlocksResults(candidateBlocks.MinedBlocks)
		if int64(len(apiCandidates)) > apiServer.config.Blocks {
			stats["candidatesSmall"] = apiCandidates[0:apiServer.config.Blocks] // Only return the last x number of blocks, defined by blocks within config.json
		} else {
			stats["candidatesSmall"] = apiCandidates
		}
		stats["candidates"] = apiCandidates // Retain full list for other use cases in load more options etc.
		numCandidateBlocks = len(candidateBlocks.MinedBlocks)
	}

	immatureBlocks := apiServer.backend.GetBlocksFound("immature")
	if immatureBlocks != nil {
		apiImmature := apiServer.convertBlocksResults(immatureBlocks.MinedBlocks)
		if int64(len(apiImmature)) > apiServer.config.Blocks {
			stats["immatureSmall"] = apiImmature[0:apiServer.config.Blocks] // Only return the last x number of blocks, defined by blocks within config.json
		} else {
			stats["immatureSmall"] = apiImmature
		}
		stats["immature"] = apiImmature // Retain full list for other use cases in load more options etc.
		numImmatureBlocks = len(immatureBlocks.MinedBlocks)
	}

	maturedBlocks := apiServer.backend.GetBlocksFound("matured")
	if maturedBlocks != nil {
		apiMatured := apiServer.convertBlocksResults(maturedBlocks.MinedBlocks)
		if int64(len(apiMatured)) > apiServer.config.Blocks {
			stats["maturedSmall"] = apiMatured[0:apiServer.config.Blocks] // Only return the last x number of blocks, defined by blocks within config.json
		} else {
			stats["maturedSmall"] = apiMatured
		}
		stats["matured"] = apiMatured // Retain full list for other use cases in load more options etc.
		numMaturedBlocks = len(maturedBlocks.MinedBlocks)
	}

	stats["candidatesTotal"] = numCandidateBlocks
	stats["immatureTotal"] = numImmatureBlocks
	stats["maturedTotal"] = numMaturedBlocks
	stats["blocksTotal"] = numCandidateBlocks + numImmatureBlocks + numMaturedBlocks

	// Build miner stats
	minerStats := apiServer.backend.GetAllMinerStats()
	apiMiners, poolHashrate, totalPoolMiners, totalPoolWorkers, soloHashrate, totalSoloMiners, totalSoloWorkers, totalRoundShares := apiServer.convertMinerResults(minerStats)
	stats["miners"] = apiMiners
	stats["poolHashrate"] = poolHashrate
	stats["totalPoolMiners"] = totalPoolMiners
	stats["totalPoolWorkers"] = totalPoolWorkers
	stats["soloHashrate"] = soloHashrate
	stats["totalSoloMiners"] = totalSoloMiners
	stats["totalSoloWorkers"] = totalSoloWorkers
	stats["totalRoundShares"] = totalRoundShares

	// Chart data
	poolHashrateChart := apiServer.backend.GetChartsData("poolhashrate")
	poolMinersChart := apiServer.backend.GetChartsData("totalpoolminers")
	poolWorkersChart := apiServer.backend.GetChartsData("totalpoolworkers")
	soloHashrateChart := apiServer.backend.GetChartsData("solohashrate")
	soloMinersChart := apiServer.backend.GetChartsData("totalsolominers")
	soloWorkersChart := apiServer.backend.GetChartsData("totalsoloworkers")
	poolDifficultyChart := apiServer.backend.GetChartsData("pooldifficulty")
	stats["poolHashrateChart"] = poolHashrateChart
	stats["poolMinersChart"] = poolMinersChart
	stats["poolWorkersChart"] = poolWorkersChart
	stats["soloHashrateChart"] = soloHashrateChart
	stats["soloMinersChart"] = soloMinersChart
	stats["soloWorkersChart"] = soloWorkersChart
	stats["poolDifficultyChart"] = poolDifficultyChart

	// Events data
	eventsData, eventsPayoutCount := apiServer.getEventsData()
	stats["eventsData"] = eventsData
	stats["eventPayoutCount"] = eventsPayoutCount
	stats["eventStartDate"] = apiServer.eventsconfig.RandomRewardEventConfig.StartDay
	stats["eventEndDate"] = apiServer.eventsconfig.RandomRewardEventConfig.EndDay
	stats["eventCriteria"] = apiServer.eventsconfig.RandomRewardEventConfig.MinerPercentCriteria
	stats["eventRewardAmount"] = apiServer.eventsconfig.RandomRewardEventConfig.RewardValueInDERO

	apiServer.stats.Store(stats)
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
				reply = &ApiPayments{Hash: value.TxHash, Timestamp: value.Timestamp, Mixin: value.Mixin, Amount: value.Amount, Fee: value.TxFee, Payees: 1}
				totalPayments++
			}
		} else {
			reply = &ApiPayments{Hash: value.TxHash, Timestamp: value.Timestamp, Mixin: value.Mixin, Amount: value.Amount, Fee: value.TxFee, Payees: 1}
			totalPayments++
		}
		apiPayments[value.TxHash] = reply
	}
	totalMinersPaid = len(tempMinerArr)

	for p := range apiPayments {
		paymentsArr = append(paymentsArr, apiPayments[p])
	}

	// Sort payments so most recent is index 0 [if preferred reverse, just swap > with <]
	sort.SliceStable(paymentsArr, func(i, j int) bool {
		return paymentsArr[i].Timestamp > paymentsArr[j].Timestamp
	})

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

	// Sort blocks so most recent is index 0 [if preferred reverse, just swap > with <]
	sort.SliceStable(blocksArr, func(i, j int) bool {
		return blocksArr[i].Timestamp > blocksArr[j].Timestamp
	})

	return blocksArr
}

func (apiServer *ApiServer) convertMinerResults(miners []*Miner) ([]*ApiMiner, int64, int64, int64, int64, int64, int64, int64) {
	apiMiners := make(map[string]*ApiMiner)
	var minersArr []*ApiMiner
	var poolHashrate int64
	var soloHashrate int64
	totalPoolMiners := make(map[string]string)
	var totalPoolWorkers int64
	totalSoloMiners := make(map[string]string)
	var totalSoloWorkers int64
	var totalRoundShares int64

	// Getting found block heights and ensuring that the total round hashes only accounts for > last height found (in event some old miner stats is present with roundhashes)
	blockHeightArr := apiServer.backend.GetBlocksFoundByHeightArr()

	// Create slice of heights that do not include solo blocks. This will be used to compare the last block found against miner heights below
	var heights []int64
	if blockHeightArr != nil {
		for height, isSolo := range blockHeightArr.Heights {
			if !isSolo {
				heights = append(heights, height)
			}
		}
		// Sort heights so most recent is index 0 [if preferred reverse, just swap > with <]
		sort.SliceStable(heights, func(i, j int) bool {
			return heights[i] > heights[j]
		})
	}

	currRoundShares := apiServer.backend.GetPoolRoundStats()
	if currRoundShares != nil {
		for _, v := range currRoundShares.RoundShares {
			totalRoundShares += v
		}
	}

	for _, currMiner := range miners {
		reply := &ApiMiner{}
		if miners != nil {
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
						if currMiner.IsSolo {
							ID = "solo~undefined"
						} else {
							ID = "undefined"
						}
					}

					// Generate struct for miner stats
					reply = &ApiMiner{
						LastBeat:      currMiner.LastBeat,
						StartedAt:     currMiner.StartedAt,
						ValidShares:   currMiner.ValidShares,
						InvalidShares: currMiner.InvalidShares,
						StaleShares:   currMiner.StaleShares,
						Accepts:       currMiner.Accepts,
						Rejects:       currMiner.Rejects,
						RoundShares:   currRoundShares.RoundShares[currMiner.Id],
						Hashrate:      Hashrate,
						Offline:       Offline,
						Id:            ID,
						Address:       currMiner.Address[0:7] + "..." + currMiner.Address[len(currMiner.Address)-5:len(currMiner.Address)],
						IsSolo:        currMiner.IsSolo,
						DonatePercent: currMiner.DonatePercent,
						DonationTotal: currMiner.DonationTotal,
					}

					apiMiners[ID+currMiner.Address] = reply

					// Compound pool stats: solo hashrate/miners and pool hashrate/miners
					if currMiner.IsSolo && !Offline {
						totalSoloMiners[currMiner.Address] = currMiner.Address
						soloHashrate += Hashrate
						totalSoloWorkers++
					} else if !Offline {
						totalPoolMiners[currMiner.Address] = currMiner.Address
						poolHashrate += Hashrate
						totalPoolWorkers++
					}
				}
			}
		}
	}
	for m := range apiMiners {
		minersArr = append(minersArr, apiMiners[m])
	}

	return minersArr, poolHashrate, int64(len(totalPoolMiners)), totalPoolWorkers, soloHashrate, int64(len(totalSoloMiners)), totalSoloWorkers, totalRoundShares
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
	stats["soloIDSeparator"] = apiServer.stratum.config.Stratum.SoloMining.AddressSeparator
	stats["hashDonationSeparator"] = apiServer.stratum.config.Stratum.DonatePercent.AddressSeparator
	stats["donationAddress"] = apiServer.stratum.donateID
	stats["donationDescription"] = apiServer.stratum.config.DonationDescription
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
		reply["payments"] = stats["paymentsSmall"]
		reply["totalPayments"] = stats["totalPayments"]
		reply["totalMinersPaid"] = stats["totalMinersPaid"]
		reply["candidates"] = stats["candidatesSmall"]
		reply["immature"] = stats["immatureSmall"]
		reply["matured"] = stats["maturedSmall"]
		reply["candidatesTotal"] = stats["candidatesTotal"]
		reply["immatureTotal"] = stats["immatureTotal"]
		reply["maturedTotal"] = stats["maturedTotal"]
		reply["blocksTotal"] = stats["blocksTotal"]
		reply["miners"] = stats["miners"]
		reply["poolHashrate"] = stats["poolHashrate"]
		reply["totalPoolMiners"] = stats["totalPoolMiners"]
		reply["totalPoolWorkers"] = stats["totalPoolWorkers"]
		reply["soloHashrate"] = stats["soloHashrate"]
		reply["totalSoloMiners"] = stats["totalSoloMiners"]
		reply["totalSoloWorkers"] = stats["totalSoloWorkers"]
		reply["totalRoundShares"] = stats["totalRoundShares"]
	}

	err := json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Printf("[API] Error serializing API response: %v", err)
		APIErrorLogger.Printf("[API] Error serializing API response: %v", err)
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
		reply["now"] = util.MakeTimestamp() / 1000
		reply["candidates"] = stats["candidates"]
		reply["immature"] = stats["immature"]
		reply["matured"] = stats["matured"]
		reply["candidatesTotal"] = stats["candidatesTotal"]
		reply["immatureTotal"] = stats["immatureTotal"]
		reply["maturedTotal"] = stats["maturedTotal"]
		reply["blocksTotal"] = stats["blocksTotal"]
	}

	err := json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Printf("[API] Error serializing API response: %v", err)
		APIErrorLogger.Printf("[API] Error serializing API response: %v", err)
	}
}

func (apiServer *ApiServer) PaymentsIndex(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})

	stats := apiServer.getStats()
	if stats != nil {
		reply["payments"] = stats["payments"]
		reply["totalPayments"] = stats["totalPayments"]
		reply["totalMinersPaid"] = stats["totalMinersPaid"]
	}

	err := json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Printf("[API] Error serializing API response: %v", err)
		APIErrorLogger.Printf("[API] Error serializing API response: %v", err)
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
		reply["miners"] = stats["miners"]
		reply["poolHashrate"] = stats["poolHashrate"]
		reply["totalPoolMiners"] = stats["totalPoolMiners"]
		reply["totalPoolWorkers"] = stats["totalPoolWorkers"]
		reply["soloHashrate"] = stats["soloHashrate"]
		reply["totalSoloMiners"] = stats["totalSoloMiners"]
		reply["totalSoloWorkers"] = stats["totalSoloWorkers"]
	}

	err := json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Printf("[API] Error serializing API response: %v", err)
		APIErrorLogger.Printf("[API] Error serializing API response: %v", err)
	}
}

func (apiServer *ApiServer) AccountIndex(writer http.ResponseWriter, r *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)

	keys, ok := r.URL.Query()["address"]

	if !ok || len(keys[0]) < 1 {
		log.Printf("URL Param 'address' is missing.")
		return
	}

	address := keys[0]

	reply := make(map[string]interface{})

	var mExist bool
	minerRegistrations := Graviton_backend.GetMinerIDRegistrations()
	for _, v := range minerRegistrations {
		if v.Address == address {
			mExist = true
			break
		}
	}

	if mExist {
		reply["address"] = address
		addrStats := apiServer.getAddressStats(address)

		reply["miners"] = addrStats["miners"]
		reply["poolHashrate"] = addrStats["poolHashrate"]
		reply["totalPoolMiners"] = addrStats["totalPoolMiners"]
		reply["totalPoolWorkers"] = addrStats["totalPoolWorkers"]
		reply["soloHashrate"] = addrStats["soloHashrate"]
		reply["totalSoloMiners"] = addrStats["totalSoloMiners"]
		reply["totalSoloWorkers"] = addrStats["totalSoloWorkers"]
		reply["payments"] = addrStats["payments"]
		reply["totalPayments"] = addrStats["totalPayments"]
		reply["pendingPayment"] = addrStats["pendingPayment"]
	} else {
		log.Printf("Address stats lookup failed. No address found for: %v", address)
	}

	/*
		stats := apiServer.getStats()
		if stats != nil {
			reply["payments"] = stats["payments"]
			reply["totalPayments"] = stats["totalPayments"]
			reply["totalMinersPaid"] = stats["totalMinersPaid"]
		}
	*/

	err := json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Printf("[API] Error serializing API response: %v", err)
		APIErrorLogger.Printf("[API] Error serializing API response: %v", err)
	}
}

func (apiServer *ApiServer) ChartsIndex(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})

	stats := apiServer.getStats()
	if stats != nil {
		reply["poolHashrateChart"] = stats["poolHashrateChart"]
		reply["poolMinersChart"] = stats["poolMinersChart"]
		reply["poolWorkersChart"] = stats["poolWorkersChart"]
		reply["soloHashrateChart"] = stats["soloHashrateChart"]
		reply["soloMinersChart"] = stats["soloMinersChart"]
		reply["soloWorkersChart"] = stats["soloWorkersChart"]
		reply["poolDifficultyChart"] = stats["poolDifficultyChart"]
	}

	err := json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Printf("[API] Error serializing API response: %v", err)
		APIErrorLogger.Printf("[API] Error serializing API response: %v", err)
	}
}

func (apiServer *ApiServer) getAddressStats(address string) map[string]interface{} {
	addressStats := make(map[string]interface{})

	// Get miners associated by address
	var addrMinerSlice []*Miner

	minerStats := apiServer.backend.GetAllMinerStats()
	if minerStats != nil {
		for _, miner := range minerStats {
			if miner.Address == address {
				addrMinerSlice = append(addrMinerSlice, miner)
			}
		}
	}

	apiMiners, poolHashrate, totalPoolMiners, totalPoolWorkers, soloHashrate, totalSoloMiners, totalSoloWorkers, _ := apiServer.convertMinerResults(addrMinerSlice)
	addressStats["miners"] = apiMiners
	addressStats["poolHashrate"] = poolHashrate
	addressStats["totalPoolMiners"] = totalPoolMiners
	addressStats["totalPoolWorkers"] = totalPoolWorkers
	addressStats["soloHashrate"] = soloHashrate
	addressStats["totalSoloMiners"] = totalSoloMiners
	addressStats["totalSoloWorkers"] = totalSoloWorkers

	// Get payments associated by address
	var addrPaymentSlice []*MinerPayments

	processedPayments := apiServer.backend.GetProcessedPayments()
	if processedPayments != nil {
		for _, payment := range processedPayments.MinerPayments {
			if payment.Login == address {
				addrPaymentSlice = append(addrPaymentSlice, payment)
			}
		}
	}

	addrProcessedPayments := &ProcessedPayments{MinerPayments: addrPaymentSlice}

	apiPayments, totalPayments, _ := apiServer.convertPaymentsResults(addrProcessedPayments)
	addressStats["payments"] = apiPayments
	addressStats["totalPayments"] = totalPayments

	// Get pending payments associated by address
	var pendingAmount uint64

	pendingPayments := apiServer.backend.GetPendingPayments()
	if pendingPayments != nil {
		for _, pending := range pendingPayments {
			if pending.Address == address {
				pendingAmount += pending.Amount
			}
		}
	}

	addressStats["pendingPayment"] = pendingAmount

	return addressStats
}

func (apiServer *ApiServer) getEventsData() ([]*ApiEventPayments, int64) {
	var eventPaymentSlice []*ApiEventPayments
	var eventPayouts int64

	if apiServer.eventsconfig.Enabled {
		// Determine pieces of the event start day
		eventStart := apiServer.eventsconfig.RandomRewardEventConfig.StartDay
		eventStartSplit := strings.Split(eventStart, "-")
		eventStartYear := eventStartSplit[0]
		eventStartYearInt, _ := strconv.Atoi(eventStartYear)
		eventStartMonth := eventStartSplit[1]
		eventStartMonthInt, _ := strconv.Atoi(eventStartMonth)
		eventStartDay, _ := strconv.Atoi(eventStartSplit[2])
		eventStartDate := time.Date(eventStartYearInt, time.Month(eventStartMonthInt), eventStartDay, 0, 0, 0, 0, time.UTC)

		// Determine pieces of the event end day
		eventEnd := apiServer.eventsconfig.RandomRewardEventConfig.EndDay
		eventEndSplit := strings.Split(eventEnd, "-")
		eventEndYear := eventEndSplit[0]
		eventEndYearInt, _ := strconv.Atoi(eventEndYear)
		eventEndMonth := eventEndSplit[1]
		eventEndMonthInt, _ := strconv.Atoi(eventEndMonth)
		eventEndDay, _ := strconv.Atoi(eventEndSplit[2])
		eventEndDate := time.Date(eventEndYearInt, time.Month(eventEndMonthInt), eventEndDay, 0, 0, 0, 0, time.UTC)

		// Determine pieces of the bonus event day, if it exists
		bonusEventStart := apiServer.eventsconfig.RandomRewardEventConfig.Bonus1hrDayEventDate
		var bonusEventStartSplit []string
		var bonusEventStartYear, bonusEventStartMonth string
		var bonusEventStartMonthInt, bonusEventStartDay int
		var bonusEventDate time.Time
		if bonusEventStart != "" {
			bonusEventStartSplit = strings.Split(bonusEventStart, "-")
			bonusEventStartYear = bonusEventStartSplit[0]
			bonusEventStartYearInt, _ := strconv.Atoi(bonusEventStartYear)
			bonusEventStartMonth = bonusEventStartSplit[1]
			bonusEventStartMonthInt, _ = strconv.Atoi(bonusEventStartMonth)
			bonusEventStartDay, _ = strconv.Atoi(bonusEventStartSplit[2])
			bonusEventDate = time.Date(bonusEventStartYearInt, time.Month(bonusEventStartMonthInt), bonusEventStartDay, 0, 0, 0, 0, time.UTC)
		}

		// Find the time inbetween event start and end, so we can generate a for() loop to go through the graviton backend [future could store in graviton a slice instead of individual, but will see]
		timeDiff := eventEndDate.Sub(eventStartDate)
		timeDiffInDays := timeDiff.Hours() / 24

		for i := 0; i < int(timeDiffInDays); i++ {
			// Assign currTime to each day to retrieve results
			var currDayTimeString, currHourTimeString string
			currTime := eventStartDate.AddDate(0, 0, i)
			currTime.Year()
			currDayTimeString = fmt.Sprintf("%v-%v-%v", currTime.Year(), int(currTime.Month()), currTime.Day())

			if currTime == bonusEventDate {
				// Get all of the hourly rewards as well
				currHourStartWindow := time.Date(currTime.Year(), currTime.Month(), currTime.Day(), 0, 0, 0, 0, time.UTC)
				for f := 0; f < 24; f++ {
					currHour := currHourStartWindow.Add(time.Hour * time.Duration(f))
					currHourTimeString = fmt.Sprintf("%v-%v-%v-%v", currHour.Year(), int(currHour.Month()), currHour.Day(), currHour.Hour())
					currHourReward := apiServer.backend.GetEventsPayment(currHourTimeString)
					if currHourReward != nil {
						replyHour := &ApiEventPayments{
							Timestamp: currHourReward.Timestamp,
							Amount:    currHourReward.Amount,
							Address:   currHourReward.Address[0:7] + "..." + currHourReward.Address[len(currHourReward.Address)-5:len(currHourReward.Address)],
						}

						eventPaymentSlice = append(eventPaymentSlice, replyHour)
						eventPayouts++
					}
				}
			}

			currDayReward := apiServer.backend.GetEventsPayment(currDayTimeString)
			if currDayReward != nil {
				replyDay := &ApiEventPayments{
					Timestamp: currDayReward.Timestamp,
					Amount:    currDayReward.Amount,
					Address:   currDayReward.Address[0:7] + "..." + currDayReward.Address[len(currDayReward.Address)-5:len(currDayReward.Address)],
				}

				eventPaymentSlice = append(eventPaymentSlice, replyDay)
				eventPayouts++
			}
		}
	}

	return eventPaymentSlice, eventPayouts
}

func (apiServer *ApiServer) EventsIndex(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=UTF-8")
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})

	stats := apiServer.getStats()
	if stats != nil {
		reply["eventsData"] = stats["eventsData"]
		reply["eventPayoutCount"] = stats["eventPayoutCount"]
		reply["eventStartDate"] = stats["eventStartDate"]
		reply["eventEndDate"] = stats["eventEndDate"]
		reply["eventCriteria"] = stats["eventCriteria"]
		reply["eventRewardAmount"] = stats["eventRewardAmount"]
	}

	err := json.NewEncoder(writer).Encode(reply)
	if err != nil {
		log.Printf("[API] Error serializing API response: %v", err)
		APIErrorLogger.Printf("[API] Error serializing API response: %v", err)
	}
}

func (apiServer *ApiServer) getStats() map[string]interface{} {
	stats := apiServer.stats.Load()
	if stats != nil {
		return stats.(map[string]interface{})
	}
	return nil
}

func logFileOutAPI(lType string) *log.Logger {
	var logFileName string
	if lType == "ERROR" {
		logFileName = "logs/apiError.log"
	} else {
		logFileName = "logs/api.log"
	}
	os.Mkdir("logs", 0705)
	f, err := os.OpenFile(logFileName, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0705)
	if err != nil {
		panic(err)
	}

	logType := lType + ": "
	l := log.New(f, logType, log.LstdFlags|log.Lmicroseconds)
	return l
}
