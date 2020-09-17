// Many unlocker integration functions and ideas from: https://github.com/JKKGBE/open-zcash-pool which is a fork of https://github.com/sammy007/open-ethereum-pool
package stratum

import (
	"fmt"
	"log"
	"math/big"
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
	backend  *RedisClient
	rpc      *rpc.RPCClient
	halt     bool
	lastFail error
}

type UnlockResult struct {
	maturedBlocks  []*BlockData
	orphanedBlocks []*BlockData
	orphans        int
	blocks         int
}

type UnlockResultGrav struct {
	maturedBlocks  []*BlockDataGrav
	orphanedBlocks []*BlockDataGrav
	orphans        int
	blocks         int
}

// Get constant blocks required to mature from derosuite
const MINER_TX_AMOUNT_UNLOCK = config.MINER_TX_AMOUNT_UNLOCK

func NewBlockUnlocker(cfg *pool.UnlockerConfig, s *StratumServer) *BlockUnlocker {
	// Ensure that config.json depth lines up with at least constant from derosuite
	/*if uint64(cfg.Depth) < MINER_TX_AMOUNT_UNLOCK {
		log.Fatalf("Block maturity depth can't be < %v, your depth is %v", MINER_TX_AMOUNT_UNLOCK, cfg.Depth)
	}*/
	u := &BlockUnlocker{config: cfg} //backend: s.backend}
	// Set blockunlocker rpc to stratumserver rpc (defined by current default upstream)
	u.rpc = s.rpc()
	return u
}

func (u *BlockUnlocker) StartBlockUnlocker(s *StratumServer) {
	log.Println("[Unlocker] Starting block unlocker")
	interval, _ := time.ParseDuration(u.config.Interval)
	timer := time.NewTimer(interval)
	log.Printf("[Unlocker] Set block unlock interval to %v", interval)

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
	if u.halt {
		log.Println("[Unlocker] Unlocking suspended due to last critical error:", u.lastFail)
		return
	}

	/*miningInfo, err := u.rpc.GetInfo()
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("Unable to get current blockchain height from node: %v", err)
		return
	}*/
	//currentHeight := miningInfo.Height

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

	/*
		candidates, err := u.backend.GetCandidates(currentHeight)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("Failed to get block candidates from backend: %v", err)
			return
		}
	*/

	// Graviton DB implementation
	resultGrav, err := u.unlockCandidatesGrav(candidateBlocks, "candidates")
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("[Unlocker] Failed to unlock blocks grav: %v", err)
		return
	}

	/*
		result, err := u.unlockCandidates(candidates, "candidates")
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("Failed to unlock blocks: %v", err)
			return
		}
	*/
	log.Printf("[Unlocker] Immature %v blocks, %v orphans", resultGrav.blocks, resultGrav.orphans)

	if len(resultGrav.orphanedBlocks) > 0 {
		err = Graviton_backend.WriteOrphanedBlocks(resultGrav.orphanedBlocks)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("[Unlocker] Failed to insert orphaned blocks into backend: %v", err)
			return
		} else {
			log.Printf("[Unlocker] Inserted %v orphaned blocks to backend", resultGrav.orphans)
		}
	}

	/*
		err = u.backend.WritePendingOrphans(result.orphanedBlocks)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("Failed to insert orphaned blocks into backend: %v", err)
			return
		} else {
			log.Printf("Inserted %v orphaned blocks to backend", result.orphans)
		}
	*/

	//totalRevenue := new(big.Rat)
	//totalMinersProfit := new(big.Rat)
	//totalPoolProfit := new(big.Rat)

	// Graviton DB
	for _, block := range resultGrav.maturedBlocks {
		err = Graviton_backend.WriteImmatureBlock(block)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("[Unlocker] Failed to credit rewards for round %v: %v", block.RoundKey(), err)
			return
		}

		log.Printf("[Unlocker] IMMATURE %v", block.RoundKey())
	}

	/*
			for _, block := range result.maturedBlocks {
				revenue, minersProfit, poolProfit, roundRewards, err := u.calculateRewards(s, block)
				if err != nil {
					u.halt = true
					u.lastFail = err
					log.Printf("Failed to calculate rewards for round %v: %v", block.RoundKey(), err)
					return
				}
				err = u.backend.WriteImmatureBlock(block, roundRewards)
				if err != nil {
					u.halt = true
					u.lastFail = err
					log.Printf("Failed to credit rewards for round %v: %v", block.RoundKey(), err)
					return
				}
				totalRevenue.Add(totalRevenue, revenue)
				totalMinersProfit.Add(totalMinersProfit, minersProfit)
				totalPoolProfit.Add(totalPoolProfit, poolProfit)

				logEntry := fmt.Sprintf(
					"IMMATURE %v: revenue %v, minersProfit %v, poolProfit %v",
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
			}
		log.Printf(
			"IMMATURE SESSION: totalRevenue %v, totalMinersProfit %v, totalPoolProfit %v",
			totalRevenue.FloatString(8),
			totalMinersProfit.FloatString(8),
			totalPoolProfit.FloatString(8),
		)*/
}

func (u *BlockUnlocker) unlockAndCreditMiners(s *StratumServer) {
	if u.halt {
		log.Println("[Unlocker] Unlocking suspended due to last critical error:", u.lastFail)
		return
	}

	miningInfo, err := u.rpc.GetInfo()
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("[Unlocker] Unable to get current blockchain height from node: %v", err)
		return
	}
	currentHeight := miningInfo.Height

	// Graviton DB
	immatureBlocksFound := Graviton_backend.GetBlocksFound("immature")

	//if len(immature) == 0 {
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

	/*
		immature, err := u.backend.GetImmatureBlocks(currentHeight - u.config.Depth)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("Failed to get immature block candidates from backend: %v", err)
			return
		}

		immatureSolo, err := u.backend.GetImmatureBlocksSolo(currentHeight - u.config.Depth)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("Failed to get immature solo block candidates from backend: %v", err)
			return
		}

		// Add on immatureSolo to the immature var so all are processed
		immature = append(immature, immatureSolo...)
	*/

	result, err := u.unlockCandidatesGrav(immature, "immature")
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("[Unlocker] Failed to unlock blocks: %v", err)
		return
	}
	/*
		result, err := u.unlockCandidates(immature, "immature")
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("Failed to unlock blocks: %v", err)
			return
		}
	*/
	log.Printf("[Unlocker] Unlocked %v blocks, %v orphans", result.blocks, result.orphans)

	if len(result.orphanedBlocks) > 0 {
		err = Graviton_backend.WriteOrphanedBlocks(result.orphanedBlocks)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("[Unlocker] Failed to insert orphaned blocks into backend: %v", err)
			return
		} else {
			log.Printf("[Unlocker] Inserted %v orphaned blocks to backend", result.orphans)
		}
	}

	/*
		for _, block := range result.orphanedBlocks {
			err = u.backend.WriteOrphan(block)
			if err != nil {
				u.halt = true
				u.lastFail = err
				log.Printf("Failed to insert orphaned block into backend: %v", err)
				return
			}
		}
		log.Printf("Inserted %v orphaned blocks to backend", result.orphans)
	*/

	totalRevenue := new(big.Rat)
	totalMinersProfit := new(big.Rat)
	totalPoolProfit := new(big.Rat)

	for _, block := range result.maturedBlocks {
		revenue, minersProfit, poolProfit, roundRewards, err := u.calculateRewardsGrav(s, block)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("[Unlocker] Failed to calculate rewards for round %v: %v", block.RoundKey(), err)
			return
		}

		err = Graviton_backend.WriteMaturedBlocks(block)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("[Unlocker] Failed to credit rewards for round %v: %v", block.RoundKey(), err)
			return
		}

		/*
			err = u.backend.WriteMaturedBlock(block, roundRewards)
			if err != nil {
				u.halt = true
				u.lastFail = err
				log.Printf("Failed to credit rewards for round %v: %v", block.RoundKey(), err)
				return
			}*/

		// Write pending payments to graviton db
		total := int64(0)
		for login, amount := range roundRewards {
			total += amount

			info := &PaymentPending{}
			info.Address = login
			info.Amount = uint64(amount)
			info.Timestamp = util.MakeTimestamp() / 1000
			infoErr := s.gravitonDB.WritePendingPayments(info)
			if infoErr != nil {
				log.Printf("[Unlocker] Graviton DB err: %v", infoErr)
			}
		}
		// To be used later, total taken from redis func, will be used for "pool" balance/payment stats
		_ = total

		totalRevenue.Add(totalRevenue, revenue)
		totalMinersProfit.Add(totalMinersProfit, minersProfit)
		totalPoolProfit.Add(totalPoolProfit, poolProfit)

		logEntry := fmt.Sprintf(
			"[Unlocker] MATURED %v: revenue %v, minersProfit %v, poolProfit %v, roundRewards %v",
			block.RoundKey(),
			revenue.FloatString(8),
			minersProfit.FloatString(8),
			poolProfit.FloatString(8),
			roundRewards,
		)
		entries := []string{logEntry}
		for login, reward := range roundRewards {
			entries = append(entries, fmt.Sprintf("\tREWARD %v: %v: %v", block.RoundKey(), login, reward))
		}
		log.Println(strings.Join(entries, "\n"))
	}

	log.Printf(
		"[Unlocker] MATURE SESSION: totalRevenue %v, totalMinersProfit %v, totalPoolProfit %v",
		totalRevenue.FloatString(8),
		totalMinersProfit.FloatString(8),
		totalPoolProfit.FloatString(8),
	)
}

func (u *BlockUnlocker) unlockCandidates(candidates []*BlockData, blockType string) (*UnlockResult, error) {
	result := &UnlockResult{}

	// Data row is: "blockHash:minerLogin:Id:Nonce:PowHash:Timestamp:Difficulty:TotalShares:CandidateKey
	for _, candidate := range candidates {
		orphan := true

		// Search for a normal block with wrong height here by traversing 16 blocks back and forward.
		//for i := int64(minDepth * -1); i < minDepth; i++ {
		//height := candidate.Height + i
		hash := candidate.Hash

		//if height < 0 {
		//	continue
		//}

		block, err := u.rpc.GetBlockByHash(hash)
		if err != nil {
			log.Printf("[Unlocker] Error while retrieving block %s from node: %v", hash, err)
			return nil, err
		}
		if block == nil {
			return nil, fmt.Errorf("[Unlocker] Error while retrieving block %s from node, wrong node hash", hash)
		}

		if matchCandidate(block, candidate) {
			orphan = false
			result.blocks++

			err = u.handleBlock(block, candidate, blockType)
			if err != nil {
				u.halt = true
				u.lastFail = err
				return nil, err
			}
			result.maturedBlocks = append(result.maturedBlocks, candidate)
			log.Printf("[Unlocker] Mature block %v with %v tx, hash: %v", candidate.Height, block.BlockHeader.Txcount, candidate.Hash)
			break
		}

		// Found block
		if !orphan {
			break
		}
		//}

		// Block is lost, we didn't find any valid block in a blockchain
		if orphan {
			result.orphans++
			candidate.Orphan = true
			result.orphanedBlocks = append(result.orphanedBlocks, candidate)
			log.Printf("[Unlocker] Orphaned block %v:%v", candidate.RoundHeight, candidate.Nonce)
		}
	}
	return result, nil
}

func (u *BlockUnlocker) unlockCandidatesGrav(candidates []*BlockDataGrav, blockType string) (*UnlockResultGrav, error) {
	result := &UnlockResultGrav{}

	// Data row is: "blockHash:minerLogin:Id:Nonce:PowHash:Timestamp:Difficulty:TotalShares:CandidateKey
	for _, candidate := range candidates {
		orphan := true

		hash := candidate.Hash

		block, err := u.rpc.GetBlockByHash(hash)
		if err != nil {
			log.Printf("[Unlocker] Error while retrieving block %s from node: %v", hash, err)
			return nil, err
		}
		if block == nil {
			return nil, fmt.Errorf("[Unlocker] Error while retrieving block %s from node, wrong node hash", hash)
		}

		if matchCandidateGrav(block, candidate) {
			orphan = false
			result.blocks++

			err = u.handleBlockGrav(block, candidate, blockType)
			if err != nil {
				u.halt = true
				u.lastFail = err
				return nil, err
			}
			result.maturedBlocks = append(result.maturedBlocks, candidate)
			log.Printf("[Unlocker] Mature block %v with %v tx, hash: %v", candidate.Height, block.BlockHeader.Txcount, candidate.Hash)
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
		}
	}
	return result, nil
}

func matchCandidate(block *rpc.GetBlockHashReply, candidate *BlockData) bool {
	return len(candidate.Hash) > 0 && strings.EqualFold(candidate.Hash, block.BlockHeader.Hash)
}

func matchCandidateGrav(block *rpc.GetBlockHashReply, candidate *BlockDataGrav) bool {
	return len(candidate.Hash) > 0 && strings.EqualFold(candidate.Hash, block.BlockHeader.Hash)
}

func (u *BlockUnlocker) handleBlock(block *rpc.GetBlockHashReply, candidate *BlockData, blockType string) error {
	//reward := util.GetConstReward(block.BlockHeader.Height)
	reward := block.BlockHeader.Reward

	// Add TX fees
	//extraTxReward, err := u.backend.GetBlockFees(block.Height, blockType)

	//if err != nil {
	//	return fmt.Errorf("error while fetching TX receipt: %v", err)
	//}

	/*if u.config.PoolFee > 0 {
		poolFee := uint64(u.config.PoolFee)
		reward = reward - poolFee
	}*/

	candidate.Height = block.BlockHeader.Height
	candidate.Orphan = false
	candidate.Hash = block.BlockHeader.Hash
	candidate.Reward = reward
	return nil
}

func (u *BlockUnlocker) handleBlockGrav(block *rpc.GetBlockHashReply, candidate *BlockDataGrav, blockType string) error {
	reward := block.BlockHeader.Reward

	candidate.Height = block.BlockHeader.Height
	candidate.Orphan = false
	candidate.Hash = block.BlockHeader.Hash
	candidate.Reward = reward
	return nil
}

/*
func (u *BlockUnlocker) calculateRewards(s *StratumServer, block *BlockData) (*big.Rat, *big.Rat, *big.Rat, map[string]int64, error) {
	revenue := new(big.Rat).SetUint64(block.Reward)
	minersProfit, poolProfit := chargeFee(revenue, u.config.PoolFee)

	//log.Printf("roundHeight: %v, Nonce: %s", block.RoundHeight, block.Nonce)
	var shares map[string]int64
	var err error

	if block.Solo {
		/*shares, err = u.backend.GetRoundSharesSolo(block.RoundHeight, block.Nonce, block.Address)
		if err != nil {
			return nil, nil, nil, nil, err
		}/
		rewards := make(map[string]int64)
		rewards[block.Address] += int64(block.Reward)
		return revenue, minersProfit, poolProfit, rewards, nil
	} else {
		shares, err = u.backend.GetRoundShares(block.RoundHeight, block.Nonce)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}

	//log.Printf("shares: %v, totalShares: %v, minersProfit: %v", shares, block.TotalShares, minersProfit)
	rewards := calculateRewardsForShares(s, shares, block.TotalShares, minersProfit)

	if block.ExtraReward != nil {
		extraReward := new(big.Rat).SetInt(block.ExtraReward)
		poolProfit.Add(poolProfit, extraReward)
		revenue.Add(revenue, extraReward)
	}

	if len(u.config.PoolFeeAddress) != 0 {
		poolProfitInt, _ := strconv.ParseInt(poolProfit.FloatString(0), 10, 64)
		rewards[u.config.PoolFeeAddress] += poolProfitInt
	}

	return revenue, minersProfit, poolProfit, rewards, nil
}

func calculateRewardsForShares(s *StratumServer, shares map[string]int64, total int64, reward *big.Rat) map[string]int64 {
	rewards := make(map[string]int64)

	for login, n := range shares {
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
	return rewards
}
*/

func (u *BlockUnlocker) calculateRewardsGrav(s *StratumServer, block *BlockDataGrav) (*big.Rat, *big.Rat, *big.Rat, map[string]int64, error) {
	// Write miner stats - force a write to ensure latest stats are in db
	log.Printf("[Unlocker] Storing miner stats")
	err := Graviton_backend.WriteMinerStats(s.miners)
	if err != nil {
		log.Printf("[Unlocker] Err storing miner stats: %v", err)
	}
	revenue := new(big.Rat).SetUint64(block.Reward)
	minersProfit, poolProfit := chargeFee(revenue, u.config.PoolFee)

	//log.Printf("roundHeight: %v, Nonce: %s", block.RoundHeight, block.Nonce)
	var shares map[string]int64
	var totalroundshares int64

	if block.Solo {
		/*shares, err = u.backend.GetRoundSharesSolo(block.RoundHeight, block.Nonce, block.Address)
		if err != nil {
			return nil, nil, nil, nil, err
		}*/
		rewards := make(map[string]int64)
		rewards[block.Address] += int64(block.Reward)
		return revenue, minersProfit, poolProfit, rewards, nil
	} else {
		shares, totalroundshares, err = Graviton_backend.GetRoundShares(block.RoundHeight, block.Nonce)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}

	//log.Printf("shares: %v, totalShares: %v, minersProfit: %v", shares, block.TotalShares, minersProfit)
	rewards := calculateRewardsForSharesGrav(s, shares, totalroundshares, minersProfit)

	if len(rewards) == 0 {
		rewards[block.Address] += int64(block.Reward)
		log.Printf("[Unlocker] No shares stored for this round, rewarding block amount (%v) to miner (%v) who found block.", block.Reward, block.Address)
	}

	if block.ExtraReward != nil {
		extraReward := new(big.Rat).SetInt(block.ExtraReward)
		poolProfit.Add(poolProfit, extraReward)
		revenue.Add(revenue, extraReward)
	}

	if len(u.config.PoolFeeAddress) != 0 {
		poolProfitInt, _ := strconv.ParseInt(poolProfit.FloatString(0), 10, 64)
		rewards[u.config.PoolFeeAddress] += poolProfitInt
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
