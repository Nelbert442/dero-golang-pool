// Many unlocker integration functions and ideas from: https://github.com/JKKGBE/open-zcash-pool which is a fork of https://github.com/sammy007/open-ethereum-pool
package stratum

import (
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/rpc"
	"git.dero.io/Nelbert442/dero-golang-pool/util"
	"github.com/deroproject/derosuite/config"
)

type BlockUnlocker struct {
	config   *pool.UnlockerConfig
	rpc      *rpc.RPCClient
	halt     bool
	lastFail error
}

type UnlockResultGrav struct {
	maturedBlocks  []*BlockDataGrav
	orphanedBlocks []*BlockDataGrav
	orphans        int
	blocks         int
}

var UnlockerInfoLogger = logFileOutUnlocker("INFO")
var UnlockerErrorLogger = logFileOutUnlocker("ERROR")

// Get constant blocks required to mature from derosuite
const MINER_TX_AMOUNT_UNLOCK = config.MINER_TX_AMOUNT_UNLOCK

func NewBlockUnlocker(cfg *pool.UnlockerConfig, s *StratumServer) *BlockUnlocker {
	u := &BlockUnlocker{config: cfg}
	// Set blockunlocker rpc to stratumserver rpc (defined by current default upstream)
	u.rpc = s.rpc()
	return u
}

func (u *BlockUnlocker) StartBlockUnlocker(s *StratumServer) {
	log.Println("[Unlocker] Starting block unlocker")
	UnlockerInfoLogger.Printf("[Unlocker] Starting block unlocker")
	interval, _ := time.ParseDuration(u.config.Interval)
	timer := time.NewTimer(interval)
	log.Printf("[Unlocker] Set block unlock interval to %v", interval)
	UnlockerInfoLogger.Printf("[Unlocker] Set block unlock interval to %v", interval)

	// Immediately unlock after start
	u.unlockPendingBlocks(s)
	u.unlockAndCreditMiners(s)
	timer.Reset(interval)

	go func() {
		for {
			select {
			case <-timer.C:
				u.unlockPendingBlocks(s)
				u.unlockAndCreditMiners(s)
				timer.Reset(interval)
			}
		}
	}()
}

func (u *BlockUnlocker) unlockPendingBlocks(s *StratumServer) {
	// Graviton DB implementation - choose to sort candidate here for faster return within storage.go, could later have "candidate" as an input and sort within GetBlocksFound() func
	blocksFound := Graviton_backend.GetBlocksFound("candidate")

	//if len(candidates) == 0 || len(candidateBlocks) == 0 {
	if blocksFound == nil {
		log.Println("[Unlocker] No block candidates to unlock")
		return
	}

	var candidateBlocks []*BlockDataGrav
	for _, value := range blocksFound.MinedBlocks {
		// This is a double check, may not be necessary but safeguarding to ensure candidate block
		if value.BlockState == "candidate" {
			candidateBlocks = append(candidateBlocks, value)
		}
	}

	if len(candidateBlocks) == 0 {
		log.Println("[Unlocker] No block candidates to unlock")
		return
	}

	// Graviton DB implementation
	resultGrav, err := u.unlockCandidatesGrav(candidateBlocks, "candidates")
	if err != nil {
		log.Printf("[Unlocker] Failed to unlock blocks grav: %v", err)
		UnlockerErrorLogger.Printf("[Unlocker] Failed to unlock blocks grav: %v", err)
		return
	}

	log.Printf("[Unlocker] Immature %v blocks, %v orphans", resultGrav.blocks, resultGrav.orphans)
	UnlockerInfoLogger.Printf("[Unlocker] Immature %v blocks, %v orphans", resultGrav.blocks, resultGrav.orphans)

	if len(resultGrav.orphanedBlocks) > 0 {
		err = Graviton_backend.WriteOrphanedBlocks(resultGrav.orphanedBlocks)
		if err != nil {
			log.Printf("[Unlocker] Failed to insert orphaned blocks into backend: %v", err)
			UnlockerErrorLogger.Printf("[Unlocker] Failed to insert orphaned blocks into backend: %v", err)
			return
		} else {
			log.Printf("[Unlocker] Inserted %v orphaned blocks to backend", resultGrav.orphans)
			UnlockerInfoLogger.Printf("[Unlocker] Inserted %v orphaned blocks to backend", resultGrav.orphans)
		}
	}

	// Graviton DB
	for _, block := range resultGrav.maturedBlocks {
		err = Graviton_backend.WriteImmatureBlock(block)
		if err != nil {
			log.Printf("[Unlocker] Failed to credit rewards for round %v: %v", block.RoundKey(), err)
			UnlockerErrorLogger.Printf("[Unlocker] Failed to credit rewards for round %v: %v", block.RoundKey(), err)
			return
		}

		log.Printf("[Unlocker] IMMATURE %v", block.RoundKey())
		UnlockerInfoLogger.Printf("[Unlocker] IMMATURE %v", block.RoundKey())
	}
}

func (u *BlockUnlocker) unlockAndCreditMiners(s *StratumServer) {
	miningInfo, err := u.rpc.GetInfo()
	if err != nil {
		log.Printf("[Unlocker] Unable to get current blockchain height from node: %v", err)
		UnlockerErrorLogger.Printf("[Unlocker] Unable to get current blockchain height from node: %v", err)
		return
	}
	currentHeight := miningInfo.Height

	// Graviton DB
	immatureBlocksFound := Graviton_backend.GetBlocksFound("immature")

	if immatureBlocksFound == nil {
		log.Println("[Unlocker] No immature blocks to credit miners")
		return
	}

	immatureBlocks := immatureBlocksFound.MinedBlocks
	var immature []*BlockDataGrav

	// Set immature to the blocks that are lower or equal to depth counter
	for _, value := range immatureBlocks {
		if value.Height <= currentHeight-u.config.Depth {
			immature = append(immature, value)
		}
	}

	if len(immature) == 0 {
		log.Println("[Unlocker] No immature blocks to credit miners")
		return
	}

	result, err := u.unlockCandidatesGrav(immature, "immature")
	if err != nil {
		log.Printf("[Unlocker] Failed to unlock blocks: %v", err)
		UnlockerErrorLogger.Printf("[Unlocker] Failed to unlock blocks: %v", err)
		return
	}
	log.Printf("[Unlocker] Unlocked %v blocks, %v orphans", result.blocks, result.orphans)
	UnlockerInfoLogger.Printf("[Unlocker] Unlocked %v blocks, %v orphans", result.blocks, result.orphans)

	if len(result.orphanedBlocks) > 0 {
		err = Graviton_backend.WriteOrphanedBlocks(result.orphanedBlocks)
		if err != nil {
			log.Printf("[Unlocker] Failed to insert orphaned blocks into backend: %v", err)
			UnlockerErrorLogger.Printf("[Unlocker] Failed to insert orphaned blocks into backend: %v", err)
			return
		} else {
			log.Printf("[Unlocker] Inserted %v orphaned blocks to backend", result.orphans)
			UnlockerInfoLogger.Printf("[Unlocker] Inserted %v orphaned blocks to backend", result.orphans)
		}
	}

	totalRevenue := new(big.Rat)
	totalMinersProfit := new(big.Rat)
	totalPoolProfit := new(big.Rat)

	for _, block := range result.maturedBlocks {
		revenue, minersProfit, poolProfit, roundRewards, err := u.calculateRewardsGrav(s, block)
		if err != nil {
			log.Printf("[Unlocker] Failed to calculate rewards for round %v: %v", block.RoundKey(), err)
			UnlockerErrorLogger.Printf("[Unlocker] Failed to calculate rewards for round %v: %v", block.RoundKey(), err)
			return
		}

		err = Graviton_backend.WriteMaturedBlocks(block)
		if err != nil {
			log.Printf("[Unlocker] Failed to credit rewards for round %v: %v", block.RoundKey(), err)
			UnlockerErrorLogger.Printf("[Unlocker] Failed to credit rewards for round %v: %v", block.RoundKey(), err)
			return
		}

		// Write pending payments to graviton db
		total := int64(0)
		for login, amount := range roundRewards {
			total += amount

			info := &PaymentPending{}
			info.Address = login
			info.Amount = uint64(amount)
			info.Timestamp = util.MakeTimestamp() / 1000
			infoErr := Graviton_backend.WritePendingPayments(info)
			if infoErr != nil {
				log.Printf("[Unlocker] Graviton DB err: %v", infoErr)
				UnlockerErrorLogger.Printf("[Unlocker] Graviton DB err: %v", infoErr)
			}
		}
		// To be used later, total taken from db func, will be used for "pool" balance/payment stats
		_ = total

		totalRevenue.Add(totalRevenue, revenue)
		totalMinersProfit.Add(totalMinersProfit, minersProfit)
		totalPoolProfit.Add(totalPoolProfit, poolProfit)

		logEntry := fmt.Sprintf(
			"[Unlocker] MATURED %v: revenue %v, minersProfit %v, poolProfit %v",
			block.RoundKey(),
			revenue.FloatString(8),
			minersProfit.FloatString(8),
			poolProfit.FloatString(8),
		)

		entries := []string{logEntry}
		for login, reward := range roundRewards {
			entries = append(entries, fmt.Sprintf("\tREWARD %v: %v: %v", block.RoundKey(), login, reward))
		}
		log.Println(strings.Join(entries, "\n"))
		UnlockerInfoLogger.Printf(strings.Join(entries, "\n"))
	}

	log.Printf(
		"[Unlocker] MATURE SESSION: totalRevenue %v, totalMinersProfit %v, totalPoolProfit %v",
		totalRevenue.FloatString(8),
		totalMinersProfit.FloatString(8),
		totalPoolProfit.FloatString(8),
	)
	UnlockerInfoLogger.Printf("[Unlocker] MATURE SESSION: totalRevenue %v, totalMinersProfit %v, totalPoolProfit %v", totalRevenue.FloatString(8), totalMinersProfit.FloatString(8), totalPoolProfit.FloatString(8))
}

func (u *BlockUnlocker) unlockCandidatesGrav(candidates []*BlockDataGrav, blockType string) (*UnlockResultGrav, error) {
	result := &UnlockResultGrav{}

	for _, candidate := range candidates {
		orphan := true

		hash := candidate.Hash

		block, err := u.rpc.GetBlockByHash(hash)
		if err != nil {
			log.Printf("[Unlocker] Error while retrieving block %s from node: %v", hash, err)
			UnlockerErrorLogger.Printf("[Unlocker] Error while retrieving block %s from node: %v", hash, err)
			return nil, err
		}
		if block == nil {
			return nil, fmt.Errorf("[Unlocker] Error while retrieving block %s from node, wrong node hash", hash)
			UnlockerErrorLogger.Printf("[Unlocker] Error while retrieving block %s from node, wrong node hash", hash)
		}

		if matchCandidateGrav(block, candidate) {
			orphan = false
			result.blocks++

			err = u.handleBlockGrav(block, candidate, blockType)
			if err != nil {
				return nil, err
			}
			result.maturedBlocks = append(result.maturedBlocks, candidate)
			log.Printf("[Unlocker] Mature block %v with %v tx, hash: %v", candidate.Height, block.BlockHeader.Txcount, candidate.Hash)
			UnlockerInfoLogger.Printf("[Unlocker] Mature block %v with %v tx, hash: %v", candidate.Height, block.BlockHeader.Txcount, candidate.Hash)
			break
		}

		// Found block
		if !orphan {
			break
		}

		// Block is lost, we didn't find any valid block in a blockchain
		if orphan {
			result.orphans++
			candidate.Orphan = true
			result.orphanedBlocks = append(result.orphanedBlocks, candidate)
			log.Printf("[Unlocker] Orphaned block %v:%v", candidate.RoundHeight, candidate.Nonce)
			UnlockerInfoLogger.Printf("[Unlocker] Orphaned block %v:%v", candidate.RoundHeight, candidate.Nonce)
		}
	}
	return result, nil
}

func matchCandidateGrav(block *rpc.GetBlockHashReply, candidate *BlockDataGrav) bool {
	return len(candidate.Hash) > 0 && strings.EqualFold(candidate.Hash, block.BlockHeader.Hash)
}

func (u *BlockUnlocker) handleBlockGrav(block *rpc.GetBlockHashReply, candidate *BlockDataGrav, blockType string) error {
	reward := block.BlockHeader.Reward

	candidate.Height = block.BlockHeader.Height
	candidate.Orphan = false
	candidate.Hash = block.BlockHeader.Hash
	candidate.Reward = reward
	return nil
}

func (u *BlockUnlocker) calculateRewardsGrav(s *StratumServer, block *BlockDataGrav) (*big.Rat, *big.Rat, *big.Rat, map[string]int64, error) {
	// Write miner stats - force a write to ensure latest stats are in db
	log.Printf("[Unlocker] Storing miner stats")
	UnlockerInfoLogger.Printf("[Unlocker] Storing miner stats")
	err := Graviton_backend.WriteMinerStats(s.miners, s.hashrateExpiration)
	if err != nil {
		log.Printf("[Unlocker] Err storing miner stats: %v", err)
		UnlockerErrorLogger.Printf("[Unlocker] Err storing miner stats: %v", err)
	}
	revenue := new(big.Rat).SetUint64(block.Reward)
	minersProfit, poolProfit := chargeFee(revenue, u.config.PoolFee)

	var shares map[string]int64
	var totalroundshares int64

	if block.Solo {
		rewards := make(map[string]int64)
		rewards[block.Address] += int64(block.Reward)
		return revenue, minersProfit, poolProfit, rewards, nil
	} else {
		shares, totalroundshares, err = Graviton_backend.GetRoundShares(block.RoundHeight)
		log.Printf("[Unlocker-calculateRewardsGrav] [round shares] shares: %v, totalroundshares: %v", shares, totalroundshares)
		UnlockerInfoLogger.Printf("[Unlocker-calculateRewardsGrav] [round shares] shares: %v, totalroundshares: %v", shares, totalroundshares)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}

	rewards := calculateRewardsForSharesGrav(s, shares, totalroundshares, minersProfit)

	if len(rewards) == 0 {
		rewards[block.Address] += int64(block.Reward)
		log.Printf("[Unlocker] No shares stored for this round, rewarding block amount (%v) to miner (%v) who found block.", block.Reward, block.Address)
		UnlockerInfoLogger.Printf("[Unlocker] No shares stored for this round, rewarding block amount (%v) to miner (%v) who found block.", block.Reward, block.Address)
	}

	if block.ExtraReward != nil {
		extraReward := new(big.Rat).SetInt(block.ExtraReward)
		poolProfit.Add(poolProfit, extraReward)
		revenue.Add(revenue, extraReward)
	}

	return revenue, minersProfit, poolProfit, rewards, nil
}

func calculateRewardsForSharesGrav(s *StratumServer, shares map[string]int64, total int64, reward *big.Rat) map[string]int64 {
	rewards := make(map[string]int64)

	for login, n := range shares {
		if n != 0 {
			// Split away for workers, paymentIDs etc. just to compound the shares associated with a given address
			address, _, paymentID, _, _ := s.splitLoginString(login)

			percent := big.NewRat(n, total)
			workerReward := new(big.Rat).Mul(reward, percent)
			workerRewardInt, _ := strconv.ParseInt(workerReward.FloatString(0), 10, 64)
			if paymentID != "" {
				combinedAddr := address + s.config.Stratum.PaymentID.AddressSeparator + paymentID
				rewards[combinedAddr] += workerRewardInt
			} else {
				rewards[address] += workerRewardInt
			}
		}
	}
	return rewards
}

// Returns new value after fee deduction and fee value.
func chargeFee(value *big.Rat, fee float64) (*big.Rat, *big.Rat) {
	feePercent := new(big.Rat).SetFloat64(fee / 100)
	feeValue := new(big.Rat).Mul(value, feePercent)
	return new(big.Rat).Sub(value, feeValue), feeValue
}

func logFileOutUnlocker(lType string) *log.Logger {
	var logFileName string
	if lType == "ERROR" {
		logFileName = "logs/unlockerError.log"
	} else {
		logFileName = "logs/unlocker.log"
	}
	os.Mkdir("logs", 0600)
	f, err := os.OpenFile(logFileName, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		panic(err)
	}

	logType := lType + ": "
	l := log.New(f, logType, log.LstdFlags|log.Lmicroseconds)
	return l
}
