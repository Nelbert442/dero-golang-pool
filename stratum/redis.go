// Many redis integration functions and ideas from: https://github.com/JKKGBE/open-zcash-pool which is a fork of https://github.com/sammy007/open-ethereum-pool
package stratum

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/util"

	//redis "github.com/go-redis/redis"
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
	Hash           string   `json:"hash"`
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

var ctx = context.Background()

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
	return join(blockData.Orphan, blockData.Nonce, blockData.serializeHash(), blockData.Timestamp, blockData.Difficulty, blockData.TotalShares, blockData.Reward)
}

func (redisClient *RedisClient) formatKey(args ...interface{}) string {
	return join(redisClient.coinPrefix, join(args...))
}

func (redisClient *RedisClient) formatRound(height int64, nonce string) string {
	return redisClient.formatKey("shares", "round"+strconv.FormatInt(height, 10), nonce)
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

func (redisClient *RedisClient) GetBalance(login string) (int64, error) {
	cmd := redisClient.client.HGet(redisClient.formatKey("miners", login), "balance")
	if cmd.Err() == redis.Nil {
		return 0, nil
	} else if cmd.Err() != nil {
		return 0, cmd.Err()
	}
	return cmd.Int64()
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

func (redisClient *RedisClient) WriteShare(login, id string, params *SubmitParams, diff int64, height int64, window time.Duration) (bool, error) {
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
		redisClient.writeShare(tx, ms, ts, login, id, diff, window)
		tx.HIncrBy(redisClient.formatKey("stats"), "roundShares", diff)
		return nil
	})
	return false, err
}

func (redisClient *RedisClient) writeShare(tx *redis.Multi, ms, ts int64, login, id string, diff int64, expire time.Duration) {
	tx.HIncrBy(redisClient.formatKey("shares", "roundCurrent"), login, diff)
	tx.ZAdd(redisClient.formatKey("hashrate"), redis.Z{Score: float64(ts), Member: join(diff, login, id, ms, diff*35)})
	tx.ZAdd(redisClient.formatKey("hashrate", login), redis.Z{Score: float64(ts), Member: join(diff, id, ms, diff*35)})
	tx.Expire(redisClient.formatKey("hashrate", login), expire) // Will delete hashrates for miners that gone
	tx.HSet(redisClient.formatKey("miners", login), "lastShare", strconv.FormatInt(ts, 10))
}

func (redisClient *RedisClient) WriteBlock(login, id string, params *SubmitParams, diff, roundDiff int64, height int64, window time.Duration, feeReward int64, blockHash string) (bool, error) {
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
		redisClient.writeShare(tx, ms, ts, login, id, diff, window)
		tx.HSet(redisClient.formatKey("stats"), "lastBlockFound", strconv.FormatInt(ts, 10))
		tx.HDel(redisClient.formatKey("stats"), "roundShares")
		tx.ZIncrBy(redisClient.formatKey("finders"), 1, login)
		tx.HIncrBy(redisClient.formatKey("miners", login), "blocksFound", 1)
		tx.Rename(redisClient.formatKey("shares", "roundCurrent"), redisClient.formatRound(int64(height), params.Id))
		tx.HGetAllMap(redisClient.formatRound(height, params.Id))
		return nil
	})
	if err != nil {
		return false, err
	} else {
		sharesMap, _ := cmds[10].(*redis.StringStringMapCmd).Result()
		totalShares := int64(0)
		for _, v := range sharesMap {
			n, _ := strconv.ParseInt(v, 10, 64)
			totalShares += n
		}
		tempParams := []string{params.Id, params.JobId, params.Nonce, params.Result}
		paramsJoined := strings.Join(tempParams, ":")
		s := join(blockHash, paramsJoined, ts, roundDiff, totalShares, feeReward)
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
	return convertCandidateResults(cmd), nil
}

func (redisClient *RedisClient) GetImmatureBlocks(maxHeight int64) ([]*BlockData, error) {
	option := redis.ZRangeByScore{Min: "0", Max: strconv.FormatInt(maxHeight, 10)}
	cmd := redisClient.client.ZRangeByScoreWithScores(redisClient.formatKey("blocks", "immature"), option)
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	return convertBlockResults(cmd), nil
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
	creditKey := redisClient.formatKey("credits", "immature", block.RoundHeight, block.Hash)
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
		tx.Rename(redisClient.formatRound(block.RoundHeight, block.Nonce), redisClient.formatRound(block.Height, block.Nonce))
	}
	tx.ZRem(redisClient.formatKey("blocks", "candidates"), block.candidateKey)
	tx.ZAdd(redisClient.formatKey("blocks", "immature"), redis.Z{Score: float64(block.Height), Member: block.key()})
}

func (redisClient *RedisClient) writeMaturedBlock(tx *redis.Multi, block *BlockData) {
	tx.Del(redisClient.formatRound(block.RoundHeight, block.Nonce))
	tx.ZRem(redisClient.formatKey("blocks", "immature"), block.immatureKey)
	tx.ZAdd(redisClient.formatKey("blocks", "matured"), redis.Z{Score: float64(block.Height), Member: block.key()})
}

func (redisClient *RedisClient) IsMinerExists(login string) (bool, error) {
	return redisClient.client.Exists(redisClient.formatKey("miners", login)).Result()
}

func (redisClient *RedisClient) GetMinerStats(login string) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	tx := redisClient.client.Multi()
	defer tx.Close()

	cmds, err := tx.Exec(func() error {
		tx.HGetAllMap(redisClient.formatKey("miners", login))
		tx.HGet(redisClient.formatKey("shares", "roundCurrent"), login)
		return nil
	})

	if err != nil && err != redis.Nil {
		return nil, err
	} else {
		result, _ := cmds[0].(*redis.StringStringMapCmd).Result()
		stats["stats"] = convertStringMap(result)
		roundShares, _ := cmds[1].(*redis.StringCmd).Int64()
		stats["roundShares"] = roundShares
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

	var c int64
	miners := make(map[string]struct{})
	max = fmt.Sprint("(", now-int64(largeWindow/time.Second))

	for {
		var keys []string
		var err error
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
		if c == 0 {
			break
		}
	}
	return total, nil
}

func convertCandidateResults(raw *redis.ZSliceCmd) []*BlockData {
	var result []*BlockData
	for _, v := range raw.Val() {
		// "blockHex:minerID:round:nonce:timestamp:diff:totalShares:extraReward"
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
		result = append(result, &block)
	}
	return result
}

func (redisClient *RedisClient) CollectStats(smallWindow time.Duration, maxBlocks int64) (map[string]interface{}, error) {
	window := int64(smallWindow / time.Second)
	stats := make(map[string]interface{})

	tx := redisClient.client.Multi()
	defer tx.Close()

	now := util.MakeTimestamp() / 1000

	cmds, err := tx.Exec(func() error {
		tx.ZRemRangeByScore(redisClient.formatKey("hashrate"), "-inf", fmt.Sprint("(", now-window))
		tx.ZRangeWithScores(redisClient.formatKey("hashrate"), 0, -1)
		tx.HGetAllMap(redisClient.formatKey("stats"))
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "candidates"), 0, -1)
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "immature"), 0, -1)
		tx.ZRevRangeWithScores(redisClient.formatKey("blocks", "matured"), 0, maxBlocks-1)
		tx.ZCard(redisClient.formatKey("blocks", "candidates"))
		tx.ZCard(redisClient.formatKey("blocks", "immature"))
		tx.ZCard(redisClient.formatKey("blocks", "matured"))
		return nil
	})

	if err != nil {
		return nil, err
	}

	result, _ := cmds[2].(*redis.StringStringMapCmd).Result()
	stats["stats"] = convertStringMap(result)
	candidates := convertCandidateResults(cmds[3].(*redis.ZSliceCmd))
	stats["candidates"] = candidates
	stats["candidatesTotal"] = cmds[6].(*redis.IntCmd).Val()

	immature := convertBlockResults(cmds[4].(*redis.ZSliceCmd))
	stats["immature"] = immature
	stats["immatureTotal"] = cmds[7].(*redis.IntCmd).Val()

	matured := convertBlockResults(cmds[5].(*redis.ZSliceCmd))
	stats["matured"] = matured
	stats["maturedTotal"] = cmds[8].(*redis.IntCmd).Val()

	totalHashrate, miners := convertMinersStats(window, cmds[1].(*redis.ZSliceCmd))
	stats["miners"] = miners
	stats["minersTotal"] = len(miners)
	stats["hashrate"] = totalHashrate
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
		tx.ZRemRangeByScore(redisClient.formatKey("hashrate", login), "-inf", fmt.Sprint("(", now-largeWindow))
		tx.ZRangeWithScores(redisClient.formatKey("hashrate", login), 0, -1)
		return nil
	})

	if err != nil {
		return nil, err
	}

	totalHashrate := int64(0)
	currentHashrate := int64(0)
	online := int64(0)
	offline := int64(0)
	workers := convertWorkersStats(smallWindow, cmds[1].(*redis.ZSliceCmd))

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
	stats["workers"] = workers
	stats["workersTotal"] = len(workers)
	stats["workersOnline"] = online
	stats["workersOffline"] = offline
	stats["hashrate"] = totalHashrate
	stats["currentHashrate"] = currentHashrate
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
		return nil
	})
	if err != nil {
		return stats, err
	}
	blocks := convertBlockResults(cmds[0].(*redis.ZSliceCmd), cmds[1].(*redis.ZSliceCmd))

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
	return stats, nil
}

func convertBlockResults(rows ...*redis.ZSliceCmd) []*BlockData {
	var result []*BlockData
	for _, row := range rows {
		for _, v := range row.Val() {
			// "orphan:nonce:blockHash:timestamp:diff:totalShares:rewardInZatoshi"
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
		shareAdjusted, _ := strconv.ParseInt(parts[3], 10, 64)
		id := parts[1]
		score := int64(v.Score)
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
	now := util.MakeTimestamp() / 1000
	miners := make(map[string]MinerData)
	totalHashrate := int64(0)

	for _, v := range raw.Val() {
		parts := strings.Split(v.Member.(string), ":")
		shareAdjusted, _ := strconv.ParseInt(parts[4], 10, 64)
		id := parts[1]
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
