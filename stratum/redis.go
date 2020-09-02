// Many redis integration functions and ideas from: https://github.com/JKKGBE/open-zcash-pool which is a fork of https://github.com/sammy007/open-ethereum-pool
package stratum

import (
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/util"

	redis "gopkg.in/redis.v3"
)

type RedisClient struct {
	client     *redis.Client
	coinPrefix string
}

type BlockData struct {
	Height         int64    `json:"height"`
	Timestamp      int64    `json:"timestamp"`
	Difficulty     int64    `json:"difficulty"`
	TotalShares    int64    `json:"shares"`
	Orphan         bool     `json:"orphan"`
	Solo           bool     `json:"solo"`
	Hash           string   `json:"hash"`
	Address        string   `json:"address"`
	Nonce          string   `json:"-"`
	PowHash        string   `json:"-"`
	Reward         uint64   `json:"-"`
	ExtraReward    *big.Int `json:"-"`
	ImmatureReward string   `json:"-"`
	RewardString   string   `json:"reward"`
	RoundHeight    int64    `json:"-"`
	candidateKey   string
	immatureKey    string
}

type MinerData struct {
	LastBeat  int64 `json:"lastBeat"`
	HR        int64 `json:"hr"`
	Offline   bool  `json:"offline"`
	startedAt int64
}

type WorkerData struct {
	MinerData
	TotalHR int64 `json:"hr2"`
}

type PendingPayment struct {
	Timestamp int64  `json:"timestamp"`
	Amount    int64  `json:"amount"`
	Address   string `json:"login"`
}

func NewRedisClient(cfg *pool.Redis, coinPrefix string) *RedisClient {
	redisAddr := fmt.Sprintf("%s:%v", cfg.Host, cfg.Port)
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,    // Defines redis addr, usually 127.0.0.1:6379
		Password: cfg.Password, // Generally pwd blank, option if required
		DB:       cfg.DB,       // Generally set to 0 for default DB
	})

	log.Printf("Redis DB Set To: %s => Index %v", redisAddr, cfg.DB)

	_, err := rdb.Ping().Result()
	if err != nil {
		log.Fatal("[Fatal] Redis DB Connection Error: ", err.Error())
	}

	log.Printf("Redis DB Connection Successful: %s", redisAddr)

	return &RedisClient{client: rdb, coinPrefix: coinPrefix}
}

func (blockData *BlockData) serializeHash() string {
	if len(blockData.Hash) > 0 {
		return blockData.Hash
	} else {
		return "0"
	}
}

func (blockData *BlockData) RoundKey() string {
	return join(blockData.RoundHeight, blockData.Hash)
}

func (blockData *BlockData) key() string {
	return join(blockData.Orphan, blockData.Nonce, blockData.serializeHash(), blockData.Timestamp, blockData.Difficulty, blockData.TotalShares, blockData.Reward, blockData.Solo, blockData.Address)
}

func (redisClient *RedisClient) formatKey(args ...interface{}) string {
	return join(redisClient.coinPrefix, join(args...))
}

func (redisClient *RedisClient) formatRound(height int64, nonce string) string {
	return redisClient.formatKey("shares", "round"+strconv.FormatInt(height, 10), nonce)
}

func (redisClient *RedisClient) formatRoundSolo(height int64, nonce string) string {
	return redisClient.formatKey("shares", "roundSolo"+strconv.FormatInt(height, 10), nonce)
}

func join(args ...interface{}) string {
	s := make([]string, len(args))
	for i, v := range args {
		switch v.(type) {
		case string:
			s[i] = v.(string)
		case int64:
			s[i] = strconv.FormatInt(v.(int64), 10)
		case uint64:
			s[i] = strconv.FormatUint(v.(uint64), 10)
		case float64:
			s[i] = strconv.FormatFloat(v.(float64), 'f', 0, 64)
		case bool:
			if v.(bool) {
				s[i] = "1"
			} else {
				s[i] = "0"
			}
		case *big.Int:
			n := v.(*big.Int)
			if n != nil {
				s[i] = n.String()
			} else {
				s[i] = "0"
			}
		default:
			panic("Invalid type specified for conversion")
		}
	}
	return strings.Join(s, ":")
}

func (redisClient *RedisClient) GetRoundShares(height int64, nonce string) (map[string]int64, error) {
	result := make(map[string]int64)

	cmd := redisClient.client.HGetAllMap(redisClient.formatRound(height, nonce))
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	sharesMap, _ := cmd.Result()
	for login, v := range sharesMap {
		n, _ := strconv.ParseInt(v, 10, 64)
		result[login] = n
	}
	return result, nil
}

/*func (redisClient *RedisClient) GetRoundSharesSolo(height int64, nonce, soloLogin string) (map[string]int64, error) {
	result := make(map[string]int64)

	cmd := redisClient.client.HGetAllMap(redisClient.formatRoundSolo(height, nonce))
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	sharesMap, _ := cmd.Result()
	for login, v := range sharesMap {
		n, _ := strconv.ParseInt(v, 10, 64)
		if login == soloLogin {
			result[login] = n
		}
	}
	return result, nil
}*/

func (redisClient *RedisClient) GetBalance(login string) (uint64, error) {
	cmd := redisClient.client.HGet(redisClient.formatKey("miners", login), "balance")
	if cmd.Err() == redis.Nil {
		return 0, nil
	} else if cmd.Err() != nil {
		return 0, cmd.Err()
	}
	return cmd.Uint64()
}

func (redisClient *RedisClient) WriteLastBlockState(id string, difficulty string, height int64, timestamp int64, reward int64, hash string) error {
	tx := redisClient.client.Multi()
	defer tx.Close()

	_, err := tx.Exec(func() error {
		tx.HSet(redisClient.formatKey("lastblock"), join(id, "difficulty"), difficulty)
		tx.HSet(redisClient.formatKey("lastblock"), join(id, "height"), strconv.FormatInt(height, 10))
		tx.HSet(redisClient.formatKey("lastblock"), join(id, "timestamp"), strconv.FormatInt(timestamp, 10))
		tx.HSet(redisClient.formatKey("lastblock"), join(id, "reward"), strconv.FormatInt(reward, 10))
		tx.HSet(redisClient.formatKey("lastblock"), join(id, "hash"), hash)
		tx.HSet(redisClient.formatKey("network"), join(id, "difficulty"), difficulty)
		tx.HSet(redisClient.formatKey("network"), join(id, "height"), strconv.FormatInt(height, 10))
		return nil
	})
	return err
}

func (redisClient *RedisClient) GetLastNetworkStates() (map[string]map[string]interface{}, error) {
	cmd := redisClient.client.HGetAllMap(redisClient.formatKey("network"))
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	m := make(map[string]map[string]interface{})
	for key, value := range cmd.Val() {
		parts := strings.Split(key, ":")
		if val, ok := m[parts[0]]; ok {
			val[parts[1]] = value
		} else {
			node := make(map[string]interface{})
			node[parts[1]] = value
			m[parts[0]] = node
		}
	}

	return m, nil
}

func (redisClient *RedisClient) GetLastBlockStates() (map[string]map[string]interface{}, error) {
	cmd := redisClient.client.HGetAllMap(redisClient.formatKey("lastblock"))
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}

	m := make(map[string]map[string]interface{})
	for key, value := range cmd.Val() {
		parts := strings.Split(key, ":")
		if val, ok := m[parts[0]]; ok {
			val[parts[1]] = value
		} else {
			node := make(map[string]interface{})
			node[parts[1]] = value
			m[parts[0]] = node
		}
	}

	return m, nil
}

func (redisClient *RedisClient) WriteNodeState(id string, height int64, diff *big.Int) error {
	tx := redisClient.client.Multi()
	defer tx.Close()

	now := util.MakeTimestamp() / 1000

	_, err := tx.Exec(func() error {
		tx.HSet(redisClient.formatKey("nodes"), join(id, "name"), id)
		tx.HSet(redisClient.formatKey("nodes"), join(id, "height"), strconv.FormatInt(height, 10))
		tx.HSet(redisClient.formatKey("nodes"), join(id, "difficulty"), diff.String())
		tx.HSet(redisClient.formatKey("nodes"), join(id, "lastBeat"), strconv.FormatInt(now, 10))
		return nil
	})
	return err
}

func (redisClient *RedisClient) GetNodeStates() ([]map[string]interface{}, error) {
	cmd := redisClient.client.HGetAllMap(redisClient.formatKey("nodes"))
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	m := make(map[string]map[string]interface{})
	for key, value := range cmd.Val() {
		parts := strings.Split(key, ":")
		if val, ok := m[parts[0]]; ok {
			val[parts[1]] = value
		} else {
			node := make(map[string]interface{})
			node[parts[1]] = value
			m[parts[0]] = node
		}
	}
	v := make([]map[string]interface{}, len(m))
	i := 0
	for _, value := range m {
		v[i] = value
		i++
	}
	return v, nil
}

func (redisClient *RedisClient) checkPoWExist(height int64, params *SubmitParams) (bool, error) {
	// Sweep PoW backlog for previous blocks, we have 3 templates back in RAM
	tempParams := []string{params.Id, params.JobId, params.Nonce, params.Result}
	redisClient.client.ZRemRangeByScore(redisClient.formatKey("pow"), "-inf", fmt.Sprint("(", height-8))
	val, err := redisClient.client.ZAdd(redisClient.formatKey("pow"), redis.Z{Score: float64(height), Member: strings.Join(tempParams, ":")}).Result()
	return val == 0, err
}

func (redisClient *RedisClient) WriteShare(login, id string, params *SubmitParams, diff int64, height int64, window time.Duration, solo bool, address string) (bool, error) {
	exist, err := redisClient.checkPoWExist(height, params)
	if err != nil {
		return false, err
	}

	if exist {
		return true, nil
	}
	tx := redisClient.client.Multi()
	defer tx.Close()

	ms := util.MakeTimestamp()
	ts := ms / 1000

	_, err = tx.Exec(func() error {
		redisClient.writeShare(tx, ms, ts, login, id, diff, window, solo, address)
		if solo {
			tx.HIncrBy(redisClient.formatKey("stats"), "roundSharesSolo", diff) // [DERO:stats] -- increments roundSharesSolo with each submitted share
		} else {
			tx.HIncrBy(redisClient.formatKey("stats"), "roundShares", diff) // [DERO:stats] -- increments roundShares with each submitted share
		}
		return nil
	})
	return false, err
}

func (redisClient *RedisClient) writeShare(tx *redis.Multi, ms, ts int64, login, id string, diff int64, expire time.Duration, solo bool, address string) {
	if solo {
		tx.HIncrBy(redisClient.formatKey("shares", "roundCurrentSolo"), login, diff)                                                  // [DERO:shares:roundCurrentSolo] -- minerID and hashes this round
		tx.ZAdd(redisClient.formatKey("hashrateSolo"), redis.Z{Score: float64(ts), Member: join(diff, login, id, ms, solo, address)}) // [DERO:hashrateSolo] -- [shareDiff:minerID:timeSubmitted] with score of timeSubmitted
		tx.ZAdd(redisClient.formatKey("hashrateSolo", login), redis.Z{Score: float64(ts), Member: join(diff, id, ms, solo, address)}) // [DERO:hashrateSolo:minerID] -- [shareDiff:id:timeSubmitted] with score of timeSubmitted
		tx.Expire(redisClient.formatKey("hashrateSolo", login), expire)                                                               // [DERO:hashrateSolo:minerID]Will delete hashrates for miners that gone
	} else {
		tx.HIncrBy(redisClient.formatKey("shares", "roundCurrent"), login, diff)                                                  // [DERO:shares:roundCurrent] -- minerID and hashes this round
		tx.ZAdd(redisClient.formatKey("hashrate"), redis.Z{Score: float64(ts), Member: join(diff, login, id, ms, solo, address)}) // [DERO:hashrate] -- [shareDiff:minerID:timeSubmitted] with score of timeSubmitted
		tx.ZAdd(redisClient.formatKey("hashrate", login), redis.Z{Score: float64(ts), Member: join(diff, id, ms, solo, address)}) // [DERO:hashrate:minerID] -- [shareDiff:id:timeSubmitted] with score of timeSubmitted
		tx.Expire(redisClient.formatKey("hashrate", login), expire)                                                               // [DERO:hashrate:minerID]Will delete hashrates for miners that gone
	}
	tx.HSet(redisClient.formatKey("miners", login), "lastShare", strconv.FormatInt(ts, 10)) // [DERO:miners:minerID] Sets time of last share submitted by login (miner id)
}

func (redisClient *RedisClient) WriteBlock(login, id string, params *SubmitParams, diff, roundDiff int64, height int64, window time.Duration, feeReward int64, blockHash string, solo bool, address string) (bool, error) {
	exist, err := redisClient.checkPoWExist(height, params)
	if err != nil {
		return false, err
	}

	if exist {
		return true, nil
	}
	tx := redisClient.client.Multi()
	defer tx.Close()

	ms := util.MakeTimestamp()
	ts := ms / 1000

	cmds, err := tx.Exec(func() error {
		redisClient.writeShare(tx, ms, ts, login, id, diff, window, solo, address)
		tx.ZIncrBy(redisClient.formatKey("finders"), 1, login)
		tx.HIncrBy(redisClient.formatKey("miners", login), "blocksFound", 1)
		if solo {
			tx.HSet(redisClient.formatKey("stats"), "lastBlockFoundSolo", strconv.FormatInt(ts, 10))
			tx.HDel(redisClient.formatKey("stats"), "roundSharesSolo")
			tx.Rename(redisClient.formatKey("shares", "roundCurrentSolo"), redisClient.formatRoundSolo(int64(height), params.Nonce))
			tx.HGetAllMap(redisClient.formatRoundSolo(height, params.Nonce))
		} else {
			tx.HSet(redisClient.formatKey("stats"), "lastBlockFound", strconv.FormatInt(ts, 10))
			tx.HDel(redisClient.formatKey("stats"), "roundShares")
			tx.Rename(redisClient.formatKey("shares", "roundCurrent"), redisClient.formatRound(int64(height), params.Nonce))
			tx.HGetAllMap(redisClient.formatRound(height, params.Nonce))
		}
		return nil
	})
	if err != nil {
		return false, err
	} else {
		var sharesMap map[string]string
		sharesMap, _ = cmds[10].(*redis.StringStringMapCmd).Result()
		totalShares := int64(0)
		for _, v := range sharesMap {
			n, _ := strconv.ParseInt(v, 10, 64)
			totalShares += n
		}
		tempParams := []string{params.Id, params.JobId, params.Nonce, params.Result}
		paramsJoined := strings.Join(tempParams, ":")
		log.Printf("paramsJoined: %v", paramsJoined)
		s := join(blockHash, paramsJoined, ts, roundDiff, totalShares, feeReward, solo, address)
		log.Printf("s: %v", s)
		cmd := redisClient.client.ZAdd(redisClient.formatKey("blocks", "candidates"), redis.Z{Score: float64(height), Member: s})
		return false, cmd.Err()
	}
}

func (redisClient *RedisClient) GetCandidates(maxHeight int64) ([]*BlockData, error) {
	option := redis.ZRangeByScore{Min: "0", Max: strconv.FormatInt(maxHeight, 10)}
	cmd := redisClient.client.ZRangeByScoreWithScores(redisClient.formatKey("blocks", "candidates"), option)
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	return convertCandidateResults(cmd, false), nil
}

func (redisClient *RedisClient) GetImmatureBlocks(maxHeight int64) ([]*BlockData, error) {
	option := redis.ZRangeByScore{Min: "0", Max: strconv.FormatInt(maxHeight, 10)}
	cmd := redisClient.client.ZRangeByScoreWithScores(redisClient.formatKey("blocks", "immature"), option)
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	return convertBlockResults(false, cmd), nil
}

func (redisClient *RedisClient) GetImmatureBlocksSolo(maxHeight int64) ([]*BlockData, error) {
	option := redis.ZRangeByScore{Min: "0", Max: strconv.FormatInt(maxHeight, 10)}
	cmd := redisClient.client.ZRangeByScoreWithScores(redisClient.formatKey("blocks", "immatureSolo"), option)
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	return convertBlockResults(false, cmd), nil
}

func (redisClient *RedisClient) WriteImmatureBlock(block *BlockData, roundRewards map[string]int64) error {
	tx := redisClient.client.Multi()
	defer tx.Close()

	_, err := tx.Exec(func() error {
		redisClient.writeImmatureBlock(tx, block)
		total := int64(0)
		for login, amount := range roundRewards {
			total += amount
			tx.HIncrBy(redisClient.formatKey("miners", login), "immature", amount)
			tx.HSetNX(redisClient.formatKey("credits", "immature", block.Height, block.Hash), login, strconv.FormatInt(amount, 10))
		}
		tx.HIncrBy(redisClient.formatKey("finances"), "immature", total)
		return nil
	})
	return err
}

func (redisClient *RedisClient) WriteMaturedBlock(block *BlockData, roundRewards map[string]int64) error {
	creditKey := redisClient.formatKey("credits", "immature", block.RoundHeight, block.Hash)
	tx, err := redisClient.client.Watch(creditKey)
	// Must decrement immatures using existing log entry
	immatureCredits := tx.HGetAllMap(creditKey)
	if err != nil {
		return err
	}
	defer tx.Close()

	ts := util.MakeTimestamp() / 1000
	value := join(block.Hash, ts, block.Reward)

	_, err = tx.Exec(func() error {
		redisClient.writeMaturedBlock(tx, block)
		tx.ZAdd(redisClient.formatKey("credits", "all"), redis.Z{Score: float64(block.Height), Member: value})

		// Decrement immature balances
		totalImmature := int64(0)
		for login, amountString := range immatureCredits.Val() {
			amount, _ := strconv.ParseInt(amountString, 10, 64)
			totalImmature += amount
			tx.HIncrBy(redisClient.formatKey("miners", login), "immature", (amount * -1))
		}

		// Increment balances
		total := int64(0)
		for login, amount := range roundRewards {
			total += amount
			// NOTICE: Maybe expire round reward entry in 604800 (a week)?
			tx.HIncrBy(redisClient.formatKey("miners", login), "balance", amount)
			tx.HSetNX(redisClient.formatKey("credits", block.Height, block.Hash), login, strconv.FormatInt(amount, 10))
		}
		tx.Del(creditKey)
		tx.HIncrBy(redisClient.formatKey("finances"), "balance", total)
		tx.HIncrBy(redisClient.formatKey("finances"), "immature", (totalImmature * -1))
		tx.HSet(redisClient.formatKey("finances"), "lastCreditHeight", strconv.FormatInt(block.Height, 10))
		tx.HSet(redisClient.formatKey("finances"), "lastCreditHash", block.Hash)
		tx.HIncrBy(redisClient.formatKey("finances"), "totalMined", int64(block.Reward))
		return nil
	})
	return err
}

func (redisClient *RedisClient) WriteOrphan(block *BlockData) error {
	var creditKey string
	if block.Solo {
		creditKey = redisClient.formatKey("credits", "immatureSolo", block.RoundHeight, block.Hash)
	} else {
		creditKey = redisClient.formatKey("credits", "immature", block.RoundHeight, block.Hash)
	}
	tx, err := redisClient.client.Watch(creditKey)
	// Must decrement immatures using existing log entry
	immatureCredits := tx.HGetAllMap(creditKey)
	if err != nil {
		return err
	}
	defer tx.Close()

	_, err = tx.Exec(func() error {
		redisClient.writeMaturedBlock(tx, block)

		// Decrement immature balances
		totalImmature := int64(0)
		for login, amountString := range immatureCredits.Val() {
			amount, _ := strconv.ParseInt(amountString, 10, 64)
			totalImmature += amount
			tx.HIncrBy(redisClient.formatKey("miners", login), "immature", (amount * -1))
		}
		tx.Del(creditKey)
		tx.HIncrBy(redisClient.formatKey("finances"), "immature", (totalImmature * -1))
		return nil
	})
	return err
}

func (redisClient *RedisClient) WritePendingOrphans(blocks []*BlockData) error {
	tx := redisClient.client.Multi()
	defer tx.Close()

	_, err := tx.Exec(func() error {
		for _, block := range blocks {
			redisClient.writeImmatureBlock(tx, block)
		}
		return nil
	})
	return err
}

func (redisClient *RedisClient) writeImmatureBlock(tx *redis.Multi, block *BlockData) {
	// Redis 2.8.x returns "ERR source and destination objects are the same"
	if block.Height != block.RoundHeight {
		if block.Solo {
			tx.Rename(redisClient.formatRoundSolo(block.RoundHeight, block.Nonce), redisClient.formatRoundSolo(block.Height, block.Nonce))
		} else {
			tx.Rename(redisClient.formatRound(block.RoundHeight, block.Nonce), redisClient.formatRound(block.Height, block.Nonce))
		}
	}
	tx.ZRem(redisClient.formatKey("blocks", "candidates"), block.candidateKey)
	if block.Solo {
		tx.ZAdd(redisClient.formatKey("blocks", "immatureSolo"), redis.Z{Score: float64(block.Height), Member: block.key()})
	} else {
		tx.ZAdd(redisClient.formatKey("blocks", "immature"), redis.Z{Score: float64(block.Height), Member: block.key()})
	}
}

func (redisClient *RedisClient) writeMaturedBlock(tx *redis.Multi, block *BlockData) {
	if block.Solo {
		tx.Del(redisClient.formatRoundSolo(block.RoundHeight, block.Nonce))                                                 // DEL "DERO:shares:roundSolo+strconv.FormatInt(block.RoundHeight, 10), block.Nonce"
		tx.ZRem(redisClient.formatKey("blocks", "immatureSolo"), block.immatureKey)                                         // ZREM "DERO:blocks:immatureSolo" block.immatureKey
		tx.ZAdd(redisClient.formatKey("blocks", "maturedSolo"), redis.Z{Score: float64(block.Height), Member: block.key()}) // ZADD "DERO:blocks:matured" ...
	} else {
		tx.Del(redisClient.formatRound(block.RoundHeight, block.Nonce))                                                 // DEL "DERO:shares:round+strconv.FormatInt(block.RoundHeight, 10), block.Nonce"
		tx.ZRem(redisClient.formatKey("blocks", "immature"), block.immatureKey)                                         // ZREM "DERO:blocks:immature" block.immatureKey
		tx.ZAdd(redisClient.formatKey("blocks", "matured"), redis.Z{Score: float64(block.Height), Member: block.key()}) // ZADD "DERO:blocks:matured" ...
	}
	//tx.ZRem(redisClient.formatKey("blocks", "immature"), block.immatureKey)                                         // ZREM "DERO:blocks:immature" block.immatureKey
	//tx.ZAdd(redisClient.formatKey("blocks", "matured"), redis.Z{Score: float64(block.Height), Member: block.key()}) // ZADD "DERO:blocks:matured" ...
}

func (redisClient *RedisClient) IsMinerExists(login string) (bool, error) {
	return redisClient.client.Exists(redisClient.formatKey("miners", login)).Result()
}

func (redisClient *RedisClient) GetMinerStats(login string, maxPayments int64) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	tx := redisClient.client.Multi()
	defer tx.Close()

	cmds, err := tx.Exec(func() error {
		tx.HGetAllMap(redisClient.formatKey("miners", login))                              // cmds[0] HGETALL "DERO:miners:login"
		tx.ZRevRangeWithScores(redisClient.formatKey("payments", login), 0, maxPayments-1) // cmds[1] ZREVRANGE "DERO:payments:login" 0 maxPayments-1
		tx.ZCard(redisClient.formatKey("payments", login))                                 // cmds[2] ZCARD "DERO:payments:login"
		tx.HGet(redisClient.formatKey("shares", "roundCurrent"), login)                    // cmds[3] HGET "DERO:shares:roundCurrent" login
		tx.HGet(redisClient.formatKey("shares", "roundCurrentSolo"), login)                // cmds[3] HGET "DERO:shares:roundCurrentSolo" login
		return nil
	})

	if err != nil && err != redis.Nil {
		return nil, err
	} else {
		result, _ := cmds[0].(*redis.StringStringMapCmd).Result()
		stats["stats"] = convertStringMap(result)
		payments := convertPaymentsResults(cmds[1].(*redis.ZSliceCmd))
		stats["payments"] = payments
		stats["paymentsTotal"] = cmds[2].(*redis.IntCmd).Val()
		roundShares, _ := cmds[3].(*redis.StringCmd).Int64()
		roundSharesSolo, _ := cmds[4].(*redis.StringCmd).Int64()
		stats["roundShares"] = roundShares
		stats["roundSharesSolo"] = roundSharesSolo
	}

	return stats, nil
}

// Try to convert all numeric strings to int64
func convertStringMap(m map[string]string) map[string]interface{} {
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

// WARNING: It must run periodically to flush out of window hashrate entries
func (redisClient *RedisClient) FlushStaleStats(window, largeWindow time.Duration) (int64, error) {
	now := util.MakeTimestamp() / 1000
	max := fmt.Sprint("(", now-int64(window/time.Second))
	total, err := redisClient.client.ZRemRangeByScore(redisClient.formatKey("hashrate"), "-inf", max).Result()
	if err != nil {
		return total, err
	}

	totalSolo, errS := redisClient.client.ZRemRangeByScore(redisClient.formatKey("hashrateSolo"), "-inf", max).Result()
	if errS != nil {
		return totalSolo, errS
	}

	var c, cS int64
	miners := make(map[string]struct{})
	max = fmt.Sprint("(", now-int64(largeWindow/time.Second))

	for {
		var keys []string
		var keysSolo []string
		var err error
		var errS error
		c, keys, err = redisClient.client.Scan(c, redisClient.formatKey("hashrate", "*"), 100).Result()
		if err != nil {
			return total, err
		}
		for _, row := range keys {
			login := strings.Split(row, ":")[2]
			if _, ok := miners[login]; !ok {
				n, err := redisClient.client.ZRemRangeByScore(redisClient.formatKey("hashrate", login), "-inf", max).Result()
				if err != nil {
					return total, err
				}
				miners[login] = struct{}{}
				total += n
			}
		}
		cS, keysSolo, errS = redisClient.client.Scan(cS, redisClient.formatKey("hashrateSolo", "*"), 100).Result()
		if errS != nil {
			return totalSolo, errS
		}
		for _, row := range keysSolo {
			login := strings.Split(row, ":")[2]
			if _, ok := miners[login]; !ok {
				n, err := redisClient.client.ZRemRangeByScore(redisClient.formatKey("hashrateSolo", login), "-inf", max).Result()
				if err != nil {
					return total, err
				}
				miners[login] = struct{}{}
				total += n
			}
		}
		if c == 0 {
			break
		}
	}
	return total, nil
}

func convertCandidateResults(raw *redis.ZSliceCmd, hideFullAddr bool) []*BlockData {
	var result []*BlockData
	for _, v := range raw.Val() {
		// "blockHex:minerID:round:nonce:_:timestamp:diff:totalShares:extraReward:solo:address"
		block := BlockData{}
		block.Height = int64(v.Score)
		block.RoundHeight = block.Height
		fields := strings.Split(v.Member.(string), ":")
		block.Hash = fields[0]
		block.Nonce = fields[3]
		block.PowHash = fields[4]
		block.Timestamp, _ = strconv.ParseInt(fields[5], 10, 64)
		block.Difficulty, _ = strconv.ParseInt(fields[6], 10, 64)
		block.TotalShares, _ = strconv.ParseInt(fields[7], 10, 64)
		//block.ExtraReward, _ = new(big.Int).SetString(fields[9], 10)
		block.candidateKey = v.Member.(string)
		block.Solo, _ = strconv.ParseBool(fields[9])
		if hideFullAddr {
			block.Address = fields[10][0:11] + "..." + fields[10][len(fields[10])-5:len(fields[10])]
		} else {
			block.Address = fields[10]
		}
		result = append(result, &block)
	}
	return result
}

func (redisClient *RedisClient) CollectStats(smallWindow time.Duration, maxBlocks, maxPayments int64) (map[string]interface{}, error) {
	window := int64(smallWindow / time.Second)
	stats := make(map[string]interface{})

	tx := redisClient.client.Multi()
	defer tx.Close()

	now := util.MakeTimestamp() / 1000

	cmds, err := tx.Exec(func() error {
		tx.ZRemRangeByScore(redisClient.formatKey("hashrate"), "-inf", fmt.Sprint("(", now-window))     // cmds[0] ZREMRANGEBYSCORE "DERO:hashrate" ...
		tx.ZRangeWithScores(redisClient.formatKey("hashrate"), 0, -1)                                   // cmds[1] ZRANGE "DERO:hashrate" 0 -1 WITHSCORES
		tx.ZRemRangeByScore(redisClient.formatKey("hashrateSolo"), "-inf", fmt.Sprint("(", now-window)) // cmds[2] ZREMRANGEBYSCORE "DERO:hashrateSolo" ...
		tx.ZRangeWithScores(redisClient.formatKey("hashrateSolo"), 0, -1)                               // cmds[3] ZRANGE "DERO:hashrateSolo" 0 -1 WITHSCORES
		tx.HGetAllMap(redisClient.formatKey("stats"))                                                   // cmds[4] HGETALL "DERO:stats"
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "candidates"), 0, -1)                    // cmds[5] ZREVRANGE "DERO:blocks:candidates" 0 -1 WITHSCORES
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "immature"), 0, -1)                      // cmds[6] ZREVRANGE "DERO:blocks:immature" 0 -1 WITHSCORES
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "immatureSolo"), 0, -1)                  // cmds[7] ZREVRANGE "DERO:blocks:immatureSolo" 0 -1 WITHSCORES
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "matured"), 0, maxBlocks-1)              // cmds[8] ZREVRANGE "DERO:blocks:matured" 0 maxBlocks-1 WITHSCORES
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "maturedSolo"), 0, maxBlocks-1)          // cmds[9] ZREVRANGE "DERO:blocks:maturedSolo" 0 maxBlocks-1 WITHSCORES
		tx.ZCard(redisClient.formatKey("blocks", "candidates"))                                         // cmds[10] ZCARD "DERO:blocks:candidates"
		tx.ZCard(redisClient.formatKey("blocks", "immature"))                                           // cmds[11] ZCARD "DERO:blocks:immature"
		tx.ZCard(redisClient.formatKey("blocks", "immatureSolo"))                                       // cmds[12] ZCARD "DERO:blocks:immatureSolo"
		tx.ZCard(redisClient.formatKey("blocks", "matured"))                                            // cmds[13] ZCARD "DERO:blocks:matured"
		tx.ZCard(redisClient.formatKey("blocks", "maturedSolo"))                                        // cmds[14] ZCARD "DERO:blocks:maturedSolo"
		tx.ZCard(redisClient.formatKey("payments", "all"))                                              // cmds[15] ZCARD "DERO:payments:all"
		tx.ZRevRangeWithScores(redisClient.formatKey("payments", "all"), 0, maxPayments-1)              // cmds[16] ZREVRANGE "DERO:payments:all" 0 maxPayments-1 WITHSCORES
		tx.ZRevRangeWithScores(redisClient.formatKey("payments", "all"), 0, -1)                         // cmds[17] ZREVRANGE "DERO:payments:all" 0 -1 WITHSCORES
		return nil
	})

	if err != nil {
		return nil, err
	}

	result, _ := cmds[4].(*redis.StringStringMapCmd).Result()
	stats["stats"] = convertStringMap(result)
	candidates := convertCandidateResults(cmds[5].(*redis.ZSliceCmd), true)
	stats["candidates"] = candidates
	stats["candidatesTotal"] = cmds[10].(*redis.IntCmd).Val()

	immature := convertBlockResults(true, cmds[6].(*redis.ZSliceCmd))
	stats["immature"] = immature
	stats["immatureTotal"] = cmds[11].(*redis.IntCmd).Val()

	immatureSolo := convertBlockResults(true, cmds[7].(*redis.ZSliceCmd))
	stats["immatureSolo"] = immatureSolo
	stats["immatureSoloTotal"] = cmds[12].(*redis.IntCmd).Val()

	matured := convertBlockResults(true, cmds[8].(*redis.ZSliceCmd))
	stats["matured"] = matured
	stats["maturedTotal"] = cmds[13].(*redis.IntCmd).Val()

	maturedSolo := convertBlockResults(true, cmds[9].(*redis.ZSliceCmd))
	stats["maturedSolo"] = maturedSolo
	stats["maturedSoloTotal"] = cmds[14].(*redis.IntCmd).Val()

	stats["totalBlocks"] = cmds[11].(*redis.IntCmd).Val() + cmds[13].(*redis.IntCmd).Val()
	stats["totalBlocksSolo"] = cmds[12].(*redis.IntCmd).Val() + cmds[14].(*redis.IntCmd).Val()

	payments := convertPaymentsResults(cmds[16].(*redis.ZSliceCmd))
	stats["payments"] = payments
	stats["paymentsTotal"] = cmds[15].(*redis.IntCmd).Val()

	totalMinersPaid := convertTotalMinersPaid(cmds[17].(*redis.ZSliceCmd))
	stats["totalMinersPaid"] = totalMinersPaid

	totalHashrate, miners := convertMinersStats(window, cmds[1].(*redis.ZSliceCmd))
	totalHashrateSolo, minersSolo := convertMinersStats(window, cmds[3].(*redis.ZSliceCmd))
	stats["miners"] = miners
	stats["minersTotal"] = len(miners)
	stats["hashrate"] = totalHashrate
	stats["minersSolo"] = minersSolo
	stats["minersTotalSolo"] = len(minersSolo)
	stats["hashrateSolo"] = totalHashrateSolo
	return stats, nil
}

func (redisClient *RedisClient) CollectWorkersStats(sWindow, lWindow time.Duration, login string) (map[string]interface{}, error) {
	smallWindow := int64(sWindow / time.Second)
	largeWindow := int64(lWindow / time.Second)
	stats := make(map[string]interface{})

	tx := redisClient.client.Multi()
	defer tx.Close()

	now := util.MakeTimestamp() / 1000

	cmds, err := tx.Exec(func() error {
		tx.ZRemRangeByScore(redisClient.formatKey("hashrate", login), "-inf", fmt.Sprint("(", now-largeWindow))     // cmds[0] ZREMRANGEBYSCORE "DERO:hashrate" ...
		tx.ZRangeWithScores(redisClient.formatKey("hashrate", login), 0, -1)                                        // cmds[1] ZRANGE "DERO:hashrate" 0 -1 WITHSCORES
		tx.ZRemRangeByScore(redisClient.formatKey("hashrateSolo", login), "-inf", fmt.Sprint("(", now-largeWindow)) // cmds[0] ZREMRANGEBYSCORE "DERO:hashrate" ...
		tx.ZRangeWithScores(redisClient.formatKey("hashrateSolo", login), 0, -1)                                    // cmds[1] ZRANGE "DERO:hashrate" 0 -1 WITHSCORES
		return nil
	})

	if err != nil {
		return nil, err
	}

	totalHashrate := int64(0)
	currentHashrate := int64(0)
	totalHashrateSolo := int64(0)
	currentHashrateSolo := int64(0)
	online := int64(0)
	offline := int64(0)
	onlineSolo := int64(0)
	offlineSolo := int64(0)
	workers := convertWorkersStats(smallWindow, cmds[1].(*redis.ZSliceCmd))
	workersSolo := convertWorkersStats(smallWindow, cmds[3].(*redis.ZSliceCmd))

	for id, worker := range workers {
		timeOnline := now - worker.startedAt
		if timeOnline < 600 {
			timeOnline = 600
		}

		boundary := timeOnline
		if timeOnline >= smallWindow {
			boundary = smallWindow
		}
		worker.HR = worker.HR / boundary

		boundary = timeOnline
		if timeOnline >= largeWindow {
			boundary = largeWindow
		}
		worker.TotalHR = worker.TotalHR / boundary

		if worker.LastBeat < (now - smallWindow/2) {
			worker.Offline = true
			offline++
		} else {
			online++
		}

		currentHashrate += worker.HR
		totalHashrate += worker.TotalHR
		workers[id] = worker
	}
	for id, workerSolo := range workersSolo {
		timeOnline := now - workerSolo.startedAt
		if timeOnline < 600 {
			timeOnline = 600
		}

		boundary := timeOnline
		if timeOnline >= smallWindow {
			boundary = smallWindow
		}
		workerSolo.HR = workerSolo.HR / boundary

		boundary = timeOnline
		if timeOnline >= largeWindow {
			boundary = largeWindow
		}
		workerSolo.TotalHR = workerSolo.TotalHR / boundary

		if workerSolo.LastBeat < (now - smallWindow/2) {
			workerSolo.Offline = true
			offlineSolo++
		} else {
			onlineSolo++
		}

		currentHashrateSolo += workerSolo.HR
		totalHashrateSolo += workerSolo.TotalHR
		workersSolo[id] = workerSolo
	}
	stats["workers"] = workers
	stats["workersTotal"] = len(workers)
	stats["workersOnline"] = online
	stats["workersOffline"] = offline
	stats["hashrate"] = totalHashrate
	stats["currentHashrate"] = currentHashrate

	stats["workersSolo"] = workersSolo
	stats["workersTotalSolo"] = len(workersSolo)
	stats["workersOnlineSolo"] = onlineSolo
	stats["workersOfflineSolo"] = offlineSolo
	stats["hashrateSolo"] = totalHashrateSolo
	stats["currentHashrateSolo"] = currentHashrateSolo

	return stats, nil
}

func (redisClient *RedisClient) CollectLuckStats(windows []int) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	tx := redisClient.client.Multi()
	defer tx.Close()

	max := int64(windows[len(windows)-1])

	cmds, err := tx.Exec(func() error {
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "immature"), 0, -1)
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "matured"), 0, max-1)
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "immatureSolo"), 0, -1)
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "maturedSolo"), 0, max-1)
		return nil
	})
	if err != nil {
		return stats, err
	}
	blocks := convertBlockResults(true, cmds[0].(*redis.ZSliceCmd), cmds[1].(*redis.ZSliceCmd))
	blocksSolo := convertBlockResults(true, cmds[2].(*redis.ZSliceCmd), cmds[3].(*redis.ZSliceCmd))

	calcLuck := func(max int) (int, float64, float64) {
		var total int
		var sharesDiff, orphans float64

		for i, block := range blocks {
			if i > (max - 1) {
				break
			}

			if block.Orphan {
				orphans++
			}
			sharesDiff += float64(block.TotalShares) / float64(block.Difficulty)
			total++
		}
		if total > 0 {
			sharesDiff /= float64(total)
			orphans /= float64(total)
		}

		return total, sharesDiff, orphans
	}
	for _, max := range windows {
		total, sharesDiff, orphanRate := calcLuck(max)
		row := map[string]float64{
			"luck": sharesDiff, "orphanRate": orphanRate,
		}
		stats[strconv.Itoa(total)] = row
		if total < max {
			break
		}
	}

	calcLuckSolo := func(max int) (int, float64, float64) {
		var totalSolo int
		var sharesDiffSolo, orphansSolo float64

		for i, blockSolo := range blocksSolo {
			if i > (max - 1) {
				break
			}

			if blockSolo.Orphan {
				orphansSolo++
			}
			sharesDiffSolo += float64(blockSolo.TotalShares) / float64(blockSolo.Difficulty)
			totalSolo++
		}
		if totalSolo > 0 {
			sharesDiffSolo /= float64(totalSolo)
			orphansSolo /= float64(totalSolo)
		}

		return totalSolo, sharesDiffSolo, orphansSolo
	}
	for _, max := range windows {
		totalSolo, sharesDiffSolo, orphanRateSolo := calcLuckSolo(max)
		row := map[string]float64{
			"luckSolo": sharesDiffSolo, "orphanRateSolo": orphanRateSolo,
		}
		stats[strconv.Itoa(totalSolo)] = row
		if totalSolo < max {
			break
		}
	}
	return stats, nil
}

func convertBlockResults(hideFullAddr bool, rows ...*redis.ZSliceCmd) []*BlockData {
	var result []*BlockData
	for _, row := range rows {
		for _, v := range row.Val() {
			// "orphan:nonce:blockHash:timestamp:diff:totalShares:rewardInDERO:solo:address"
			block := BlockData{}
			block.Height = int64(v.Score)
			block.RoundHeight = block.Height
			fields := strings.Split(v.Member.(string), ":")
			block.Orphan, _ = strconv.ParseBool(fields[0])
			block.Nonce = fields[1]
			block.Hash = fields[2]
			block.Timestamp, _ = strconv.ParseInt(fields[3], 10, 64)
			block.Difficulty, _ = strconv.ParseInt(fields[4], 10, 64)
			block.TotalShares, _ = strconv.ParseInt(fields[5], 10, 64)
			block.RewardString = fields[6]
			block.ImmatureReward = fields[6]
			block.immatureKey = v.Member.(string)
			block.Solo, _ = strconv.ParseBool(fields[7])
			if hideFullAddr {
				block.Address = fields[8][0:11] + "..." + fields[8][len(fields[8])-5:len(fields[8])]
			} else {
				block.Address = fields[8]
			}
			result = append(result, &block)
		}
	}
	return result
}

// Build per login workers's total shares map {'rig-1': 12345, 'rig-2': 6789, ...}
// TS => diff, id, ms
func convertWorkersStats(window int64, raw *redis.ZSliceCmd) map[string]WorkerData {
	now := util.MakeTimestamp() / 1000
	workers := make(map[string]WorkerData)

	for _, v := range raw.Val() {
		parts := strings.Split(v.Member.(string), ":")
		shareAdjusted, _ := strconv.ParseInt(parts[0], 10, 64)
		id := parts[1]
		score := int64(v.Score) // timestamp of share
		worker := workers[id]

		// Add for large window
		worker.TotalHR += shareAdjusted

		// Add for small window if matches
		if score >= now-window {
			worker.HR += shareAdjusted
		}
		if worker.LastBeat < score {
			worker.LastBeat = score
		}
		if worker.startedAt > score || worker.startedAt == 0 {
			worker.startedAt = score
		}
		workers[id] = worker
	}

	return workers
}

func convertMinersStats(window int64, raw *redis.ZSliceCmd) (int64, map[string]MinerData) {
	var id string
	now := util.MakeTimestamp() / 1000
	miners := make(map[string]MinerData)
	totalHashrate := int64(0)

	for _, v := range raw.Val() {
		parts := strings.Split(v.Member.(string), ":")
		shareAdjusted, _ := strconv.ParseInt(parts[0], 10, 64)
		//id := parts[1]
		workIndex := strings.Index(parts[1], "@")
		if workIndex != -1 {
			id = parts[1][0:11] + "..." + parts[1][workIndex-5:workIndex]
		} else {
			id = parts[1][0:11] + "..." + parts[1][len(parts[1])-5:len(parts[1])]
		}
		score := int64(v.Score)
		miner := miners[id]
		miner.HR += shareAdjusted

		if miner.LastBeat < score {
			miner.LastBeat = score
		}
		if miner.startedAt > score || miner.startedAt == 0 {
			miner.startedAt = score
		}
		miners[id] = miner
	}

	for id, miner := range miners {
		timeOnline := now - miner.startedAt
		if timeOnline < 600 {
			timeOnline = 600
		}

		boundary := timeOnline
		if timeOnline >= window {
			boundary = window
		}
		miner.HR = miner.HR / boundary

		if miner.LastBeat < (now - window/2) {
			miner.Offline = true
		}
		totalHashrate += miner.HR
		miners[id] = miner
	}
	return totalHashrate, miners
}

func (r *RedisClient) GetPendingPayments() []*PendingPayment {
	raw := r.client.ZRevRangeWithScores(r.formatKey("payments", "pending"), 0, -1)
	var result []*PendingPayment
	for _, v := range raw.Val() {
		// timestamp -> "address:amount"
		payment := PendingPayment{}
		payment.Timestamp = int64(v.Score)
		fields := strings.Split(v.Member.(string), ":")
		payment.Address = fields[0]
		payment.Amount, _ = strconv.ParseInt(fields[1], 10, 64)
		result = append(result, &payment)
	}
	return result
}

func (r *RedisClient) IsPayoutsLocked() (bool, error) {
	_, err := r.client.Get(r.formatKey("payments", "lock")).Result()
	if err == redis.Nil {
		return false, nil
	} else if err != nil {
		return false, err
	} else {
		return true, nil
	}
}

func (r *RedisClient) GetPayees() ([]string, error) {
	payees := make(map[string]struct{})
	var result []string
	var c int64

	for {
		var keys []string
		var err error
		c, keys, err = r.client.Scan(c, r.formatKey("miners", "*"), 100).Result()
		if err != nil {
			return nil, err
		}
		for _, row := range keys {
			login := strings.Split(row, ":")[2]
			payees[login] = struct{}{}
		}
		if c == 0 {
			break
		}
	}
	for login, _ := range payees {
		result = append(result, login)
	}
	return result, nil
}

func (r *RedisClient) LockPayouts(login string, amount int64) error {
	key := r.formatKey("payments", "lock")
	result := r.client.SetNX(key, join(login, amount), 0).Val()
	if !result {
		return fmt.Errorf("Unable to acquire lock '%s'", key)
	}
	return nil
}

func (r *RedisClient) UnlockPayouts() error {
	key := r.formatKey("payments", "lock")
	_, err := r.client.Del(key).Result()
	return err
}

// Deduct miner's balance for payment
func (r *RedisClient) UpdateBalance(login string, amount int64) error {
	tx := r.client.Multi()
	defer tx.Close()

	ts := util.MakeTimestamp() / 1000

	_, err := tx.Exec(func() error {
		tx.HIncrBy(r.formatKey("miners", login), "balance", (amount * -1))
		tx.HIncrBy(r.formatKey("miners", login), "pending", amount)
		tx.HIncrBy(r.formatKey("finances"), "balance", (amount * -1))
		tx.HIncrBy(r.formatKey("finances"), "pending", amount)
		tx.ZAdd(r.formatKey("payments", "pending"), redis.Z{Score: float64(ts), Member: join(login, amount)})
		return nil
	})
	return err
}

func (r *RedisClient) RollbackBalance(login string, amount int64) error {
	tx := r.client.Multi()
	defer tx.Close()

	_, err := tx.Exec(func() error {
		tx.HIncrBy(r.formatKey("miners", login), "balance", amount)
		tx.HIncrBy(r.formatKey("miners", login), "pending", (amount * -1))
		tx.HIncrBy(r.formatKey("finances"), "balance", amount)
		tx.HIncrBy(r.formatKey("finances"), "pending", (amount * -1))
		tx.ZRem(r.formatKey("payments", "pending"), join(login, amount))
		return nil
	})
	return err
}

func (r *RedisClient) WritePayment(login, txHash, txKey string, txFee, mixin uint64, amount int64) error {
	tx := r.client.Multi()
	defer tx.Close()

	ts := util.MakeTimestamp() / 1000

	_, err := tx.Exec(func() error {
		tx.HIncrBy(r.formatKey("miners", login), "pending", (amount * -1))
		tx.HIncrBy(r.formatKey("miners", login), "paid", amount)
		tx.HIncrBy(r.formatKey("finances"), "pending", (amount * -1))
		tx.HIncrBy(r.formatKey("finances"), "paid", amount)
		tx.ZAdd(r.formatKey("payments", "all"), redis.Z{Score: float64(ts), Member: join(txHash, login, amount, txKey, txFee, mixin)})
		tx.ZAdd(r.formatKey("payments", login), redis.Z{Score: float64(ts), Member: join(txHash, amount, txKey, txFee, mixin)})
		tx.ZRem(r.formatKey("payments", "pending"), join(login, amount))
		tx.Del(r.formatKey("payments", "lock"))
		return nil
	})
	return err
}

func (r *RedisClient) BgSave() (string, error) {
	return r.client.BgSave().Result()
}

// Defines output of .../api/payments
func convertPaymentsResults(raw *redis.ZSliceCmd) []map[string]interface{} {
	var result []map[string]interface{}
	for _, v := range raw.Val() {
		tx := make(map[string]interface{})
		tx["timestamp"] = int64(v.Score)
		fields := strings.Split(v.Member.(string), ":")
		tx["tx"] = fields[0]
		// Individual or whole payments row
		if len(fields) < 6 {
			// txHash:amount:txKey:txFee:mixin -- with score of timestamp
			tx["amount"], _ = strconv.ParseInt(fields[1], 10, 64)
			tx["fee"], _ = strconv.ParseInt(fields[3], 10, 64)
			tx["mixin"], _ = strconv.ParseInt(fields[4], 10, 64)
		} else {
			// txHash:login:amount:txKey:txFee:mixin -- with score of timestamp
			workIndex := strings.Index(fields[1], "@")
			if workIndex != -1 {
				tx["address"] = fields[1][0:11] + "..." + fields[1][workIndex-5:workIndex]
			} else {
				tx["address"] = fields[1][0:11] + "..." + fields[1][len(fields[1])-5:len(fields[1])]
			}
			//tx["address"] = fields[1][0:7] + "..." + fields[1][len(fields[1])-7:len(fields[1])]
			tx["amount"], _ = strconv.ParseInt(fields[2], 10, 64)
			tx["fee"], _ = strconv.ParseInt(fields[4], 10, 64)
			tx["mixin"], _ = strconv.ParseInt(fields[5], 10, 64)
		}
		result = append(result, tx)
	}
	return result
}

// Counts unique addresses from all payments
func convertTotalMinersPaid(raw *redis.ZSliceCmd) int {
	var tempAddr []string
	for _, v := range raw.Val() {
		//tx := make(map[string]interface{})
		fields := strings.Split(v.Member.(string), ":")
		// Individual or whole payments row
		if len(fields) >= 6 {
			// txHash:login:amount:txKey:txFee:mixin -- with score of timestamp
			workIndex := strings.Index(fields[1], "@")
			if workIndex != -1 {
				tempAddr = append(tempAddr, fields[1][0:11]+"..."+fields[1][workIndex-5:workIndex])
			} else {
				tempAddr = append(tempAddr, fields[1][0:11]+"..."+fields[1][len(fields[1])-5:len(fields[1])])
			}
		}
	}

	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range tempAddr {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}

	return len(list)
}
