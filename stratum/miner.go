package stratum

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Nelbert442/dero-golang-pool/rpc"
	"github.com/Nelbert442/dero-golang-pool/util"
)

type Job struct {
	height uint64
	sync.RWMutex
	id          string
	extraNonce  uint32
	submissions map[string]struct{}
}

type Miner struct {
	LastBeat        int64
	StartedAt       int64
	ValidShares     int64
	InvalidShares   int64
	StaleShares     int64
	TrustedShares   int64
	Accepts         int64
	Rejects         int64
	Shares          map[int64]int64
	LastRoundShares int64
	RoundShares     int64
	RoundHeight     int64
	Hashrate        int64
	Offline         bool
	sync.RWMutex
	Id            string
	Address       string
	PaymentID     string
	FixedDiff     uint64
	IsSolo        bool
	WorkID        string
	Ip            string
	DonatePercent int64
	DonationTotal int64
}

var MinerInfoLogger = logFileOutMiner("INFO")
var MinerErrorLogger = logFileOutMiner("ERROR")

func (job *Job) submit(nonce string) bool {
	job.Lock()
	defer job.Unlock()
	if _, exist := job.submissions[nonce]; exist {
		return true
	}
	job.submissions[nonce] = struct{}{}
	return false
}

func NewMiner(id string, address string, paymentid string, fixedDiff uint64, workID string, donationPercent int64, isSolo bool, ip string) *Miner {
	shares := make(map[int64]int64)
	now := util.MakeTimestamp() / 1000
	return &Miner{Id: id, Address: address, PaymentID: paymentid, FixedDiff: fixedDiff, IsSolo: isSolo, WorkID: workID, DonatePercent: donationPercent, Ip: ip, Shares: shares, StartedAt: now}
}

func (cs *Session) calcVarDiff(currDiff float64, s *StratumServer) int64 {
	var newDiff float64
	timestamp := time.Now().Unix()

	variance := s.config.Stratum.VarDiff.VariancePercent / 100 * float64(s.config.Stratum.VarDiff.TargetTime)
	tMin := float64(s.config.Stratum.VarDiff.TargetTime) - variance
	tMax := float64(s.config.Stratum.VarDiff.TargetTime) + variance

	// Set last time varDiff config was handled, usually done initially and builds the map for timestamparr
	if cs.VarDiff.LastRetargetTimestamp == 0 {
		cs.VarDiff.LastRetargetTimestamp = timestamp - s.config.Stratum.VarDiff.RetargetTime/2
		cs.VarDiff.LastTimeStamp = timestamp
		cs.VarDiff.TimestampArr = make(map[int64]int64)

		return int64(currDiff)
	}

	if (timestamp - cs.VarDiff.LastRetargetTimestamp) < s.config.Stratum.VarDiff.RetargetTime {
		return int64(currDiff)
	}

	if len(cs.VarDiff.TimestampArr) <= 0 {
		sinceLast := timestamp - cs.VarDiff.LastTimeStamp
		cs.VarDiff.TimestampArr[sinceLast] += sinceLast
	}
	cs.VarDiff.LastRetargetTimestamp = timestamp

	var avg float64
	var sum int64
	for _, v := range cs.VarDiff.TimestampArr {
		sum = sum + v
	}

	avg = float64(sum) / float64(len(cs.VarDiff.TimestampArr))

	diffCalc := float64(s.config.Stratum.VarDiff.TargetTime) / avg

	if avg > tMax && currDiff >= float64(s.config.Stratum.VarDiff.MinDiff) {
		if diffCalc*currDiff < float64(s.config.Stratum.VarDiff.MinDiff) {
			diffCalc = float64(s.config.Stratum.VarDiff.MinDiff) / currDiff
		}
	} else if avg < tMin {
		diffMax := float64(s.config.Stratum.VarDiff.MaxDiff)

		if diffCalc*currDiff > diffMax {
			diffCalc = diffMax / currDiff
		}
	} else {
		return int64(currDiff)
	}

	newDiff = currDiff * diffCalc

	if newDiff <= 0 {
		newDiff = currDiff
	}

	maxJump := s.config.Stratum.VarDiff.MaxJump / 100 * currDiff

	// Prevent diff scale up/down to be more than maxJump %.
	if newDiff > currDiff && !(newDiff-maxJump <= currDiff) {
		newDiff = currDiff + maxJump
	} else if currDiff > newDiff && !(newDiff+(maxJump) >= currDiff) {
		newDiff = currDiff - (maxJump)
	}

	// Reset timestampArr
	cs.VarDiff.TimestampArr = make(map[int64]int64)

	return int64(newDiff)
}

func (cs *Session) getJob(t *BlockTemplate, s *StratumServer, diff int64) *JobReplyData {
	if diff == 0 {
		diff = cs.difficulty
	}

	lastBlockHeight := cs.lastBlockHeight
	if lastBlockHeight == t.Height {
		return &JobReplyData{}
	}

	// Define difficulty and set targetHex = util.GetTargetHex(cs.difficulty) else targetHex == cs.endpoint.targetHex
	var targetHex string

	if diff != 0 && cs.isFixedDiff { // If fixed difficulty is defined
		if diff >= cs.endpoint.config.MinDiff {
			targetHex = util.GetTargetHex(diff)
		} else {
			targetHex = util.GetTargetHex(cs.endpoint.config.MinDiff)
		}
	} else { // If vardiff is enabled, otherwise use the default value of the session
		if s.config.Stratum.VarDiff.Enabled == true {

			targetHex = util.GetTargetHex(diff)
		} else { // If not fixed diff and vardiff is not enabled, use default config difficulty and targetHex
			targetHex = cs.endpoint.targetHex
		}
	}

	extraNonce := atomic.AddUint32(&cs.endpoint.extraNonce, 1)
	blob := t.nextBlob(extraNonce, cs.endpoint.instanceId)
	id := atomic.AddUint64(&cs.endpoint.jobSequence, 1)
	job := &Job{
		id:         strconv.FormatUint(id, 10),
		extraNonce: extraNonce,
		height:     t.Height,
	}
	job.submissions = make(map[string]struct{})
	cs.pushJob(job)
	reply := &JobReplyData{JobId: job.id, Blob: blob, Target: targetHex, Algo: s.config.Algo, Height: t.Height}
	return reply
}

func (cs *Session) pushJob(job *Job) {
	cs.Lock()
	defer cs.Unlock()
	cs.validJobs = append(cs.validJobs, job)

	if len(cs.validJobs) > 4 {
		cs.validJobs = cs.validJobs[1:]
	}
}

func (cs *Session) findJob(id string) *Job {
	cs.Lock()
	defer cs.Unlock()
	for _, job := range cs.validJobs {
		if job.id == id {
			return job
		}
	}
	return nil
}

func (m *Miner) heartbeat() {
	now := util.MakeTimestamp() / 1000
	atomic.StoreInt64(&m.LastBeat, now)
}

func (m *Miner) storeShare(diff, minershares, templateHeight int64) {
	now := util.MakeTimestamp() / 1000

	if m.IsSolo {
		// If miner is solo, we don't care about updating roundheight/roundshares etc. These vals aren't used as upon a solo block being found, the address who finds get all rewards
		// Just normal tracking of shares for hashrate purposes
		m.Lock()
		m.Shares[now] += diff
		m.Unlock()
	} else {

		blockHeightArr := Graviton_backend.GetBlocksFoundByHeightArr()
		var resetVars bool

		if blockHeightArr != nil {
			for height, _ := range blockHeightArr.Heights {
				if atomic.LoadInt64(&m.RoundHeight) != 0 && atomic.LoadInt64(&m.RoundHeight) <= height {
					// Miner round height is less than a pre-found block [usually happens for disconnected miners && new rounds]. Reset counters
					m.Lock()
					atomic.StoreInt64(&m.RoundHeight, templateHeight)
					// No need to add blank diff shares to m.Shares. Usually only 0 if running NextRound from storage.go
					if diff != 0 {
						m.Shares[now] += diff
					}
					atomic.StoreInt64(&m.LastRoundShares, atomic.LoadInt64(&m.RoundShares))
					atomic.StoreInt64(&m.RoundShares, minershares)
					m.Unlock()
					resetVars = true
				}
			}
		}

		if !resetVars {
			m.Lock()
			atomic.StoreInt64(&m.RoundHeight, templateHeight)
			// No need to add blank diff shares to m.Shares. Usually only 0 if running NextRound from storage.go
			if diff != 0 {
				m.Shares[now] += diff
			}
			atomic.AddInt64(&m.RoundShares, minershares)
			m.Unlock()
		}
	}
}

func (m *Miner) getHashrate(estimationWindow, hashrateExpiration time.Duration) int64 {
	now := util.MakeTimestamp() / 1000
	totalShares := int64(0)
	// Convert time window (such as 10m) to seconds
	window := int64(estimationWindow / time.Second)
	boundary := now - m.StartedAt

	if boundary >= window {
		boundary = window
	}

	m.Lock()
	// Total shares only keeping track of last hashrateExpiration time (config.json var)

	hashExpiration := int64(hashrateExpiration / time.Second)

	for k, v := range m.Shares {
		if k < now-hashExpiration {
			delete(m.Shares, k)
		} else if k >= now-boundary {
			totalShares += v
		}
	}

	m.Unlock()

	return int64(float64(totalShares) / float64(boundary))
}

func (m *Miner) processShare(s *StratumServer, cs *Session, job *Job, t *BlockTemplate, nonce string, params *SubmitParams) (bool, string) {

	// Var definitions
	var extraMinerMessage string
	var checkPowHashBig bool
	var success bool
	var bypassShareValidation bool
	var result string = params.Result
	var shareType string
	var hashBytes []byte
	var diff big.Int
	var donation float64
	diff.SetUint64(t.Difficulty)
	var setDiff big.Int
	setDiff.SetUint64(uint64(cs.difficulty))
	r := s.rpc()

	shareBuff := make([]byte, len(t.Buffer))
	copy(shareBuff, t.Buffer)
	copy(shareBuff[t.Reserved_Offset+4:t.Reserved_Offset+7], cs.endpoint.instanceId)

	extraBuff := new(bytes.Buffer)
	binary.Write(extraBuff, binary.BigEndian, job.extraNonce)
	copy(shareBuff[t.Reserved_Offset:], extraBuff.Bytes())

	nonceBuff, _ := hex.DecodeString(nonce)
	copy(shareBuff[39:], nonceBuff)

	// After trustedSharesCount is hit (number of accepted shares in a row based on config.json), hash validation will be skipped until an incorrect hash is submitted
	if atomic.LoadInt64(&m.TrustedShares) >= s.trustedSharesCount {
		shareType = "Trusted"
	} else {
		shareType = "Valid"
	}

	// Append share type, solo or pool for logging assistance
	if m.IsSolo {
		shareType = shareType + " SOLO"
	} else {
		shareType = shareType + " POOL"
	}

	hashBytes, _ = hex.DecodeString(result)

	if s.config.BypassShareValidation || shareType == "Trusted SOLO" || shareType == "Trusted POOL" {
		bypassShareValidation = true
	} else {
		switch s.algo {
		case "astrobwt":
			checkPowHashBig, success = util.AstroBWTHash(shareBuff, diff, setDiff)

			if !success {
				minerOutput := "Bad hash. If you see often [> 1/10 shares on avg], check input on miner software."
				log.Printf("[Miner] Bad hash from miner %v@%v", m.Id, cs.ip)
				MinerErrorLogger.Printf("[Miner] Bad hash from miner %v@%v", m.Id, cs.ip)

				if shareType == "Trusted" {
					log.Printf("[Miner] Miner is no longer submitting trusted shares: %v@%v", m.Id, cs.ip)
					MinerErrorLogger.Printf("[Miner] Miner is no longer submitting trusted shares: %v@%v", m.Id, cs.ip)
					shareType = "Valid"
				}

				atomic.AddInt64(&m.InvalidShares, 1)
				atomic.StoreInt64(&m.TrustedShares, 0)
				return false, minerOutput
			}

			atomic.AddInt64(&m.TrustedShares, 1)
		case "cryptonight":
			checkPowHashBig = util.CryptonightHash(shareBuff, diff)

			atomic.AddInt64(&m.TrustedShares, 1)
		default:
			// Handle when no algo is defined or unhandled algo is defined, let miner know issues (properly gets sent back in job detail rejection message)
			minerOutput := "Rejected share, no pool algo defined. Contact pool owner."
			log.Printf("[Miner] Rejected share, no pool algo defined (%s). Contact pool owner - from %v@%v", s.algo, m.Id, cs.ip)
			MinerErrorLogger.Printf("[Miner] Rejected share, no pool algo defined (%s). Contact pool owner - from %v@%v", s.algo, m.Id, cs.ip)
			return false, minerOutput
		}
	}

	hashDiff, ok := util.GetHashDifficulty(hashBytes)
	if !ok {
		minerOutput := "Bad hash"
		log.Printf("[Miner] Bad hash from miner %v@%v", m.Id, cs.ip)
		MinerErrorLogger.Printf("[Miner] Bad hash from miner %v@%v", m.Id, cs.ip)
		atomic.AddInt64(&m.InvalidShares, 1)
		return false, minerOutput
	}

	// May be redundant, or use instead of CheckPowHashBig in future.
	block := hashDiff.Cmp(&diff) >= 0

	// If bypassing share validation (either with true/false of config or miner is trusted), block should define properly if a block is found and can set checkPowHashBig to true. Perhaps future improvements to be made here
	if block && bypassShareValidation {
		checkPowHashBig = true
	}

	if checkPowHashBig && block {
		blockSubmit, err := r.SubmitBlock(t.Blocktemplate_blob, hex.EncodeToString(shareBuff))
		var blockSubmitReply *rpc.SubmitBlock_Result = &rpc.SubmitBlock_Result{}

		if blockSubmit != nil {
			if blockSubmit.Result != nil {
				err = json.Unmarshal(*blockSubmit.Result, &blockSubmitReply)
			}
		}

		if err != nil || blockSubmitReply.Status != "OK" {
			atomic.AddInt64(&m.Rejects, 1)
			atomic.AddInt64(&r.Rejects, 1)
			log.Printf("[BLOCK] Block rejected at height %d: %v", t.Height, err)
			MinerErrorLogger.Printf("[BLOCK] Block rejected at height %d: %v", t.Height, err)
			return false, "Bad hash"
		} else {
			log.Printf("[BLOCK] Block accepted. Hash: %s, Status: %s", blockSubmitReply.BLID, blockSubmitReply.Status)
			MinerInfoLogger.Printf("[BLOCK] Block accepted. Hash: %s, Status: %s", blockSubmitReply.BLID, blockSubmitReply.Status)

			now := util.MakeTimestamp() / 1000

			atomic.AddInt64(&m.Accepts, 1)
			atomic.AddInt64(&r.Accepts, 1)
			atomic.StoreInt64(&r.LastSubmissionAt, now)

			if m.IsSolo {
				log.Printf("[BLOCK] SOLO Block found at height %d, diff: %v, blid: %s, by miner: %v@%v", t.Height, t.Difficulty, blockSubmitReply.BLID, m.Id, cs.ip)
				MinerInfoLogger.Printf("[BLOCK] SOLO Block found at height %d, diff: %v, blid: %s, by miner: %v@%v", t.Height, t.Difficulty, blockSubmitReply.BLID, m.Id, cs.ip)
			} else {
				log.Printf("[BLOCK] POOL Block found at height %d, diff: %v, blid: %s, by miner: %v@%v", t.Height, t.Difficulty, blockSubmitReply.BLID, m.Id, cs.ip)
				MinerInfoLogger.Printf("[BLOCK] POOL Block found at height %d, diff: %v, blid: %s, by miner: %v@%v", t.Height, t.Difficulty, blockSubmitReply.BLID, m.Id, cs.ip)
			}

			// Immediately refresh current BT and send new jobs
			s.refreshBlockTemplate(true)

			// Graviton store of successful block
			// This could be 'cleaned' to one-liners etc., but just depends on how you feel. Upon build/testing was simpler to view in-line for spec values
			ts := util.MakeTimestamp() / 1000
			info := &BlockDataGrav{}
			info.Height = int64(t.Height)
			info.RoundHeight = int64(t.Height)
			info.Hash = blockSubmitReply.BLID
			info.Nonce = params.Nonce
			info.PowHash = result
			info.Timestamp = ts
			info.Difficulty = int64(t.Difficulty)
			// TotalShares val will be gotten from DB
			info.TotalShares = 0
			info.Solo = m.IsSolo
			info.Address = m.Address
			info.BlockState = "candidate"

			writeWait, _ := time.ParseDuration("10ms")
			for Graviton_backend.Writing == 1 {
				//log.Printf("[Miner-processShare] GravitonDB is writing... sleeping for %v...", writeWait)
				//StorageInfoLogger.Printf("[Miner-processShare] GravitonDB is writing... sleeping for %v...", writeWait)
				time.Sleep(writeWait)
			}
			Graviton_backend.Writing = 1
			infoErr := Graviton_backend.WriteBlocks(info, info.BlockState)
			Graviton_backend.Writing = 0
			if infoErr != nil {
				log.Printf("[BLOCK] Graviton DB err: %v", infoErr)
				MinerErrorLogger.Printf("[BLOCK] Graviton DB err: %v", infoErr)
			}

			var minerShare int64
			if m.DonatePercent > 0 && m.Address != s.donateID {
				donation = float64(m.DonatePercent) / 100 * float64(cs.difficulty)
				atomic.AddInt64(&m.DonationTotal, int64(donation))

				donateMiner, ok := s.miners.Get(s.donateID)
				if !ok {
					log.Printf("[Miner] Miner %v@%v intended to donate %v shares, however donation miner is not setup.", params.Id, cs.ip, int64(donation))
					MinerErrorLogger.Printf("[Miner] Miner %v@%v intended to donate %v shares, however donation miner is not setup.", params.Id, cs.ip, int64(donation))
				} else {
					log.Printf("[Miner] Miner %v@%v donated %v shares.", params.Id, cs.ip, int64(donation))
					MinerErrorLogger.Printf("[Miner] Miner %v@%v donated %v shares.", params.Id, cs.ip, int64(donation))
					donateMiner.storeShare(cs.difficulty, int64(donation), int64(t.Height))
				}

				minerShare = cs.difficulty - int64(donation)
			} else {
				minerShare = cs.difficulty
			}

			m.Lock()
			atomic.StoreInt64(&m.RoundHeight, int64(t.Height))
			// No need to add blank diff shares to m.Shares. Usually only 0 if running NextRound from storage.go
			if cs.difficulty != 0 {
				m.Shares[now] += cs.difficulty
			}

			atomic.AddInt64(&m.LastRoundShares, atomic.LoadInt64(&m.RoundShares)+minerShare)
			atomic.StoreInt64(&m.RoundShares, 0)
			m.Unlock()

			// Only update next round miner stats if a pool block is found, so can determine this by the miner who found the block's solo status
			if !m.IsSolo {
				log.Printf("[Miner] Updating miner stats in DB for current round...")
				MinerInfoLogger.Printf("[Miner] Updating miner stats in DB for current round...")

				writeWait, _ := time.ParseDuration("10ms")
				for Graviton_backend.Writing == 1 {
					//log.Printf("[Miner-processShare] GravitonDB is writing... sleeping for %v...", writeWait)
					//StorageInfoLogger.Printf("[Miner-processShare] GravitonDB is writing... sleeping for %v...", writeWait)
					time.Sleep(writeWait)
				}
				Graviton_backend.Writing = 1
				_ = Graviton_backend.WriteMinerStats(s.miners, s.hashrateExpiration)

				log.Printf("[Miner] Updating miner stats for the next round...")
				MinerInfoLogger.Printf("[Miner] Updating miner stats for the next round...")
				Graviton_backend.NextRound(int64(t.Height), s.hashrateExpiration)
				Graviton_backend.Writing = 0
			}

			//atomic.StoreInt64(&m.LastRoundShares, 0)	// Don't believe this is necessary, happens in nextround
		}
	} else if hashDiff.Cmp(&setDiff) < 0 {
		minerOutput := "Low difficulty share"
		log.Printf("[Miner] Rejected low difficulty share of %v from %v@%v", hashDiff, m.Id, cs.ip)
		MinerErrorLogger.Printf("[Miner] Rejected low difficulty share of %v from %v@%v", hashDiff, m.Id, cs.ip)
		atomic.AddInt64(&m.InvalidShares, 1)
		return false, minerOutput
	}

	// Using minermap to store share data rather than direct to DB, future scale might have issues with the large concurrent writes to DB directly
	// Minermap allows for concurrent writes easily and quickly, then every x seconds defined in stratum that map gets written/stored to disk DB [5 seconds prob]

	// Store share for current height and current round shares on normal basis. If block && checkPowHashBig, miner round share has already been counted, no need to double count here
	if !block && !checkPowHashBig {
		// If miner is donating, take % out of cs.difficulty (share amount stored) and storeShare to donation addr
		if m.DonatePercent > 0 && m.Address != s.donateID {
			donation = float64(m.DonatePercent) / 100 * float64(cs.difficulty)
			atomic.AddInt64(&m.DonationTotal, int64(donation))

			donateMiner, ok := s.miners.Get(s.donateID)
			if !ok {
				log.Printf("[Miner] Miner %v@%v intended to donate %v shares, however donation miner is not setup.", params.Id, cs.ip, int64(donation))
				MinerErrorLogger.Printf("[Miner] Miner %v@%v intended to donate %v shares, however donation miner is not setup.", params.Id, cs.ip, int64(donation))
			} else {
				log.Printf("[Miner] Miner %v@%v donated %v shares.", params.Id, cs.ip, int64(donation))
				MinerErrorLogger.Printf("[Miner] Miner %v@%v donated %v shares.", params.Id, cs.ip, int64(donation))
				donateMiner.storeShare(int64(donation), int64(donation), int64(t.Height))
			}

			minerShare := cs.difficulty - int64(donation)
			m.storeShare(cs.difficulty, minerShare, int64(t.Height))
		} else {
			m.storeShare(cs.difficulty, cs.difficulty, int64(t.Height))
		}
	} else {
		// Add extra miner message to return back to mining software if a block is found by the miner - only certain miner software will read/use these results
		if m.IsSolo {
			extraMinerMessage = fmt.Sprintf("SOLO Block found at height %d, diff: %v, by you!", t.Height, t.Difficulty)
		} else {
			extraMinerMessage = fmt.Sprintf("POOL Block found at height %d, diff: %v, by you!", t.Height, t.Difficulty)
		}
	}

	atomic.AddInt64(&m.ValidShares, 1)

	log.Printf("[Miner] %s share at difficulty %v/%v from %v@%v", shareType, cs.difficulty, hashDiff, params.Id, cs.ip)
	MinerInfoLogger.Printf("[Miner] %s share at difficulty %v/%v from %v@%v", shareType, cs.difficulty, hashDiff, params.Id, cs.ip)

	ts := time.Now().Unix()
	// Omit the first round to setup the vars if they aren't setup, otherwise commit to timestamparr
	if cs.VarDiff.LastRetargetTimestamp == 0 {
		cs.VarDiff.LastRetargetTimestamp = ts - s.config.Stratum.VarDiff.RetargetTime/2
		cs.VarDiff.LastTimeStamp = ts
		cs.VarDiff.TimestampArr = make(map[int64]int64)
	} else {
		sinceLast := ts - cs.VarDiff.LastTimeStamp
		cs.VarDiff.TimestampArr[sinceLast] += sinceLast
		cs.VarDiff.LastTimeStamp = ts
	}

	s.miners.Set(m.Id, m)

	if m.DonatePercent > 0 && m.Address != s.donateID {
		// Append if there is previous extra miner message being sent - like found block etc.
		if extraMinerMessage != "" {
			extraMinerMessage = fmt.Sprintf("%s -- Donated %v shares - Thank you!", extraMinerMessage, int64(donation))
		} else {
			extraMinerMessage = fmt.Sprintf("Donated %v shares - Thank you!", int64(donation))
		}
	}

	return true, extraMinerMessage
}

func logFileOutMiner(lType string) *log.Logger {
	var logFileName string
	if lType == "ERROR" {
		logFileName = "logs/minerError.log"
	} else {
		logFileName = "logs/miner.log"
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
