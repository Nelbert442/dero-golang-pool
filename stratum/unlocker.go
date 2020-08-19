package stratum

import (
	"log"
	"math/big"
	"strconv"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/rpc"
	"github.com/deroproject/derosuite/config"
)

type BlockUnlocker struct {
	config   *pool.UnlockerConfig
	backend  *RedisClient
	rpc      *rpc.RPCClient
	halt     bool
	lastFail error
}

// Get constant blocks required to mature from derosuite
const MINER_TX_AMOUNT_UNLOCK = config.MINER_TX_AMOUNT_UNLOCK

func NewBlockUnlocker(cfg *pool.UnlockerConfig, s *StratumServer) *BlockUnlocker {
	// Ensure that config.json depth lines up with at least constant from derosuite
	if cfg.Depth < MINER_TX_AMOUNT_UNLOCK {
		log.Fatalf("Block maturity depth can't be < %v, your depth is %v", MINER_TX_AMOUNT_UNLOCK, cfg.Depth)
	}
	u := &BlockUnlocker{config: cfg, backend: s.backend}
	// Set blockunlocker rpc to stratumserver rpc (defined by current default upstream)
	u.rpc = s.rpc()
	return u
}

func (u *BlockUnlocker) StartBlockUnlocker() {
	log.Println("Starting block unlocker")
	//interval := util.MustParseDuration(u.config.Interval)
	interval, _ := time.ParseDuration(u.config.Interval)
	timer := time.NewTimer(interval)
	log.Printf("Set block unlock interval to %v", interval)

	// Immediately unlock after start
	//u.unlockPendingBlocks()
	//u.unlockAndCreditMiners()
	timer.Reset(interval)

	go func() {
		for {
			select {
			case <-timer.C:
				log.Printf("I would be checking for pending blocks")
				//u.unlockPendingBlocks()
				//u.unlockAndCreditMiners()
				timer.Reset(interval)
			}
		}
	}()
}

func calculateRewardsForShares(shares map[string]int64, total int64, reward *big.Rat) map[string]int64 {
	rewards := make(map[string]int64)

	for login, n := range shares {
		percent := big.NewRat(n, total)
		workerReward := new(big.Rat).Mul(reward, percent)
		workerRewardInt, _ := strconv.ParseInt(workerReward.FloatString(0), 10, 64)
		rewards[login] += workerRewardInt
	}
	return rewards
}

// Returns new value after fee deduction and fee value.
func chargeFee(value *big.Rat, fee float64) (*big.Rat, *big.Rat) {
	feePercent := new(big.Rat).SetFloat64(fee / 100)
	feeValue := new(big.Rat).Mul(value, feePercent)
	return new(big.Rat).Sub(value, feeValue), feeValue
}
