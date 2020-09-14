package stratum

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"log"
	"math/big"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/rpc"
	"git.dero.io/Nelbert442/dero-golang-pool/util"
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
	LastRoundShares map[int64]int64
	RoundShares     int64
	RoundHeight     int64
	Hashrate        int64
	sync.RWMutex
	Id        string
	Address   string
	PaymentID string
	FixedDiff uint64
	IsSolo    bool
	Ip        string
}

func (job *Job) submit(nonce string) bool {
	job.Lock()
	defer job.Unlock()
	if _, exist := job.submissions[nonce]; exist {
		return true
	}
	job.submissions[nonce] = struct{}{}
	return false
}

func NewMiner(id string, address string, paymentid string, fixedDiff uint64, isSolo bool, ip string) *Miner {
	shares := make(map[int64]int64)
	lastRoundShares := make(map[int64]int64)
	return &Miner{Id: id, Address: address, PaymentID: paymentid, FixedDiff: fixedDiff, IsSolo: isSolo, Ip: ip, Shares: shares, LastRoundShares: lastRoundShares}
}

func (cs *Session) calcVarDiff(currDiff float64, s *StratumServer) int64 {
	var newDiff float64
	timestamp := time.Now().Unix()

	variance := s.config.Stratum.VarDiff.VariancePercent / 100 * float64(s.config.Stratum.VarDiff.TargetTime)
	tMin := float64(s.config.Stratum.VarDiff.TargetTime) * (1 + variance)
	tMax := float64(s.config.Stratum.VarDiff.TargetTime) * (1 - variance)

	// Set last time varDiff config was handled, usually done initially and builds the map for timestamparr
	if cs.VarDiff.LastRetargetTimestamp == 0 {
		cs.VarDiff.LastRetargetTimestamp = timestamp - s.config.Stratum.VarDiff.RetargetTime/2
		cs.VarDiff.LastTimeStamp = timestamp
		cs.VarDiff.TimestampArr = make(map[int64]int64)

		return int64(currDiff)
	}

	cs.VarDiff.LastTimeStamp = timestamp

	if (timestamp - cs.VarDiff.LastRetargetTimestamp) < s.config.Stratum.VarDiff.RetargetTime {
		return int64(currDiff)
	}

	cs.VarDiff.LastRetargetTimestamp = timestamp
	cs.VarDiff.TimestampArr[timestamp] += timestamp

	var avg float64
	var sum int64
	for _, v := range cs.VarDiff.TimestampArr {
		sum = sum + v
	}

	avg = float64(sum) / float64(len(cs.VarDiff.TimestampArr))

	log.Printf("[VARDIFF] targettime (%v) / avg (%v)", s.config.Stratum.VarDiff.TargetTime, avg)
	diff := float64(time.Duration(s.config.Stratum.VarDiff.TargetTime)*time.Second) / (avg * currDiff)

	if avg > tMax && currDiff > float64(s.config.Stratum.VarDiff.MinDiff) {
		if diff*currDiff < float64(s.config.Stratum.VarDiff.MinDiff) {
			log.Printf("[VARDIFF] minDiff (%v) / currDiff (%v)", s.config.Stratum.VarDiff.MinDiff, currDiff)
			diff = float64(s.config.Stratum.VarDiff.MinDiff) / currDiff
		}
	} else if avg < tMin {
		diffMax := float64(s.config.Stratum.VarDiff.MaxDiff)

		if diff*currDiff > diffMax {
			log.Printf("[VARDIFF] diffMax (%v) / currDiff (%v)", diffMax, currDiff)
			diff = diffMax / currDiff
		}
	} else {
		return int64(currDiff)
	}

	log.Printf("[VARDIFF] currDif (%v) * diff (%v)", currDiff, diff)
	newDiff = currDiff * diff

	if newDiff <= 0 {
		newDiff = currDiff
	}

	// Reset timestampArr
	cs.VarDiff.TimestampArr = make(map[int64]int64)

	return int64(newDiff)
}

func (cs *Session) getJob(t *BlockTemplate, s *StratumServer) (*JobReplyData, int64) {
	lastBlockHeight := cs.lastBlockHeight
	if lastBlockHeight == t.Height {
		return &JobReplyData{}, cs.difficulty
	}

	// Define difficulty and set targetHex = util.GetTargetHex(cs.difficulty) else targetHex == cs.endpoint.targetHex
	var targetHex string
	newDiff := cs.difficulty

	if cs.difficulty != 0 && cs.isFixedDiff { // If fixed difficulty is defined
		if cs.difficulty >= cs.endpoint.config.MinDiff {
			targetHex = util.GetTargetHex(cs.difficulty)
		} else {
			targetHex = util.GetTargetHex(cs.endpoint.config.MinDiff)
		}
	} else { // If vardiff is enabled, otherwise use the default value of the session
		if s.config.Stratum.VarDiff.Enabled == true {
			newDiff = cs.calcVarDiff(float64(cs.difficulty), s)

			targetHex = util.GetTargetHex(newDiff)
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
	reply := &JobReplyData{JobId: job.id, Blob: blob, Target: targetHex}
	return reply, newDiff
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
	now := util.MakeTimestamp()
	atomic.StoreInt64(&m.LastBeat, now)
}

/*
// Unused atm
func (m *Miner) getLastBeat() int64 {
	return atomic.LoadInt64(&m.lastBeat)
}
*/

func (m *Miner) storeShare(diff, templateHeight int64) {
	now := util.MakeTimestamp() / 1000

	if m.IsSolo {
		// If miner is solo, we don't care about updating roundheight/roundshares etc. These vals aren't used as upon a solo block being found, the address who finds get all rewards
		// Just normal tracking of shares for hashrate purposes
		m.Shares[now] += diff
	} else {

		blockHeightArr := Graviton_backend.GetBlocksFoundByHeightArr()
		var resetVars bool

		if blockHeightArr != nil {
			for _, v := range blockHeightArr.Heights {
				if m.RoundHeight <= v {
					// Miner round height is less than a pre-found block [usually happens for disconnected miners && new rounds]. Reset counters
					m.Lock()
					m.RoundHeight = templateHeight
					// No need to add blank diff shares to m.Shares. Usually only 0 if running NextRound from storage.go
					if diff != 0 {
						m.Shares[now] += diff
					}
					m.LastRoundShares[v] += m.RoundShares
					m.RoundShares = diff
					m.Unlock()
					resetVars = true
				}
			}
		}

		if !resetVars {
			m.Lock()
			m.RoundHeight = templateHeight
			// No need to add blank diff shares to m.Shares. Usually only 0 if running NextRound from storage.go
			if diff != 0 {
				m.Shares[now] += diff
			}
			m.RoundShares += diff
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

	if boundary > window {
		boundary = window
	}

	m.Lock()
	// Total shares only keeping track of last hashrateExpiration time (config.json var)
	hashExpiration := int64(hashrateExpiration / time.Second)
	for k, v := range m.Shares {
		if k < now-hashExpiration {
			log.Printf("Deleting shares older than %v. Timestamp: %v, Value: %v", hashExpiration, k, v)
			delete(m.Shares, k)
		} else if k >= now-window {
			totalShares += v
		}
		//log.Printf("[hashrate] totalShares: %v, minerShares: %v, window: %v", totalShares, m.Shares, window)
	}
	m.Unlock()
	return int64(float64(totalShares) / float64(window))
}

func (m *Miner) processShare(s *StratumServer, cs *Session, job *Job, t *BlockTemplate, nonce string, params *SubmitParams) (bool, string) {

	/*
		temp := Graviton_backend.GetMinerStatsByID(m.Id)
		if temp != nil {
			log.Printf("[Miner] Miner: %v", temp)
		}
	*/

	// Var definitions
	checkPowHashBig := false
	success := false
	var result string = params.Result
	var shareType string
	var hashBytes []byte
	var diff big.Int
	diff.SetUint64(t.Difficulty)
	var setDiff big.Int
	setDiff.SetInt64(cs.difficulty)
	r := s.rpc()

	shareBuff := make([]byte, len(t.Buffer))
	copy(shareBuff, t.Buffer)
	copy(shareBuff[t.Reserved_Offset+4:t.Reserved_Offset+7], cs.endpoint.instanceId)

	extraBuff := new(bytes.Buffer)
	binary.Write(extraBuff, binary.BigEndian, job.extraNonce)
	copy(shareBuff[t.Reserved_Offset:], extraBuff.Bytes())

	nonceBuff, _ := hex.DecodeString(nonce)
	copy(shareBuff[39:], nonceBuff)

	if m.TrustedShares >= s.trustedSharesCount {
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

	if s.config.BypassShareValidation || shareType == "Trusted" {
		checkPowHashBig = true
	} else {
		switch s.algo {
		case "astrobwt":
			checkPowHashBig, success = util.AstroBWTHash(shareBuff, diff)

			if !success {
				minerOutput := "Incorrect PoW - if you see often [> 1/100 shares on avg], check input on miner software"
				log.Printf("Bad hash from miner %v@%v", m.Id, cs.ip)

				if shareType == "Trusted" {
					log.Printf("[No Trust] Miner is no longer submitting trusted shares: %v@%v", m.Id, cs.ip)
					shareType = "Valid"
				}

				atomic.AddInt64(&m.InvalidShares, 1)
				atomic.StoreInt64(&m.TrustedShares, 0)
				return false, minerOutput
			}

			atomic.AddInt64(&m.TrustedShares, 1)

		default:
			// Handle when no algo is defined or unhandled algo is defined, let miner know issues (properly gets sent back in job detail rejection message)
			minerOutput := "Rejected share, no pool algo defined. Contact pool owner."
			log.Printf("Rejected share, no pool algo defined (%s). Contact pool owner - from %v@%v", s.algo, m.Id, cs.ip)
			return false, minerOutput
		}
	}

	hashDiff, ok := util.GetHashDifficulty(hashBytes)
	//log.Printf("[processShare] hashDiff: %v", hashDiff)
	if !ok {
		minerOutput := "Bad hash"
		log.Printf("Bad hash from miner %v@%v", m.Id, cs.ip)
		atomic.AddInt64(&m.InvalidShares, 1)
		return false, minerOutput
	}

	// May be redundant, or use instead of CheckPowHashBig in future.
	block := hashDiff.Cmp(&diff) >= 0

	if checkPowHashBig && block {
		blockSubmit, err := r.SubmitBlock(t.Blocktemplate_blob, hex.EncodeToString(shareBuff))
		var blockSubmitReply *rpc.SubmitBlock_Result

		if blockSubmit.Result != nil {
			err = json.Unmarshal(*blockSubmit.Result, &blockSubmitReply)
		}
		log.Printf("Block accepted. Hash: %s, Status: %s", blockSubmitReply.BLID, blockSubmitReply.Status)

		if err != nil || blockSubmitReply.Status != "OK" {
			atomic.AddInt64(&m.Rejects, 1)
			atomic.AddInt64(&r.Rejects, 1)
			log.Printf("Block rejected at height %d: %v", t.Height, err)
		} else {
			now := util.MakeTimestamp()
			// Restarts roundShares counter and returns last round num to roundShares var
			// Get round shares from db instead (incase stratum has been restarted within a given round)
			//roundShares := atomic.SwapInt64(&s.roundShares, 0)
			// Returns the ratio of total roundshares to the difficulty of the template found
			//ratio := float64(roundShares) / float64(int64(t.Difficulty))
			//s.blocksMu.Lock()
			//s.blockStats[now] = blockEntry{height: int64(t.Height), hash: blockSubmitReply.BLID, variance: ratio}
			//s.blocksMu.Unlock()
			atomic.AddInt64(&m.Accepts, 1)
			atomic.AddInt64(&r.Accepts, 1)
			atomic.StoreInt64(&r.LastSubmissionAt, now)
			if m.IsSolo {
				//log.Printf("SOLO Block found at height %d, diff: %v, blid: %s, by miner: %v@%v, ratio: %.4f", t.Height, t.Difficulty, blockSubmitReply.BLID, m.Id, cs.ip, ratio)
				log.Printf("SOLO Block found at height %d, diff: %v, blid: %s, by miner: %v@%v", t.Height, t.Difficulty, blockSubmitReply.BLID, m.Id, cs.ip)
			} else {
				//log.Printf("POOL Block found at height %d, diff: %v, blid: %s, by miner: %v@%v, ratio: %.4f", t.Height, t.Difficulty, blockSubmitReply.BLID, m.Id, cs.ip, ratio)
				log.Printf("POOL Block found at height %d, diff: %v, blid: %s, by miner: %v@%v", t.Height, t.Difficulty, blockSubmitReply.BLID, m.Id, cs.ip)
			}

			// Immediately refresh current BT and send new jobs
			s.refreshBlockTemplate(true)

			// Graviton store of successful block
			// This could be 'cleaned' to one-liners etc., but just depends on how you feel. Upon build/testing was simpler to view in-line for spec values
			ms := util.MakeTimestamp()
			ts := ms / 1000
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
			infoErr := s.gravitonDB.WriteBlocks(info, info.BlockState)
			if infoErr != nil {
				log.Printf("Graviton DB err: %v", infoErr)
			}

			log.Printf("Updating miner stats for the next round...")
			_ = s.gravitonDB.NextRound(int64(t.Height), s.miners)

			// Redis store of successful block
			/*
				_, err := s.backend.WriteBlock(params.Id, params.JobId, params, cs.difficulty, int64(t.Difficulty), int64(t.Height), s.hashrateExpiration, 0, blockSubmitReply.BLID, m.isSolo, m.address)
				if err != nil {
					log.Println("Failed to insert block data into backend:", err)
				}
			*/
		}
		//} else if hashDiff.Cmp(cs.endpoint.difficulty) < 0 {
	} else if hashDiff.Cmp(&setDiff) < 0 {
		minerOutput := "Low difficulty share"
		log.Printf("Rejected low difficulty share of %v from %v@%v", hashDiff, m.Id, cs.ip)
		atomic.AddInt64(&m.InvalidShares, 1)
		return false, minerOutput
	}

	// Using minermap to store share data rather than direct to DB, future scale might have issues with the large concurrent writes to DB directly
	// Minermap allows for concurrent writes easily and quickly, then every x seconds defined in stratum that map gets written/stored to disk DB [5 seconds prob]
	atomic.AddInt64(&s.roundShares, cs.difficulty)
	atomic.AddInt64(&m.ValidShares, 1)
	m.storeShare(cs.difficulty, int64(t.Height))
	atomic.StoreInt64(&m.Hashrate, m.getHashrate(s.estimationWindow, s.hashrateExpiration))

	// Redis store of successful share
	/*
		info := &MiningShare{}
		info.MinerID = params.Id
		info.MinerJobID = params.JobId
		info.MinerJobNonce = params.Nonce
		info.MinerJobResult = params.Result
		info.SessionDiff = cs.difficulty
		info.BlockTemplateHeight = int64(t.Height)
		info.HashrateExpiration = s.hashrateExpiration
		info.MinerIsSolo = m.isSolo
		info.MinerAddress = m.address
		infoErr := s.gravitonDB.WriteMinerSharesInMem(info)
		//infoErr := s.boltdb.WriteMinerShares(info)
		if infoErr != nil {
			log.Printf("Graviton DB err: %v", infoErr)
		}
	*/
	/*
		_, err := s.backend.WriteShare(params.Id, params.JobId, params, cs.difficulty, int64(t.Height), s.hashrateExpiration, m.IsSolo, m.Address)
		if err != nil {
			log.Println("Failed to insert share data into backend:", err)
		}
	*/

	log.Printf("%s share at difficulty %v/%v from %v@%v", shareType, cs.difficulty, hashDiff, params.Id, cs.ip)
	log.Printf("roundShares: %v, roundHeight: %v, totalshares: %v, hashrate: %v", m.RoundShares, m.RoundHeight, m.Shares, m.Hashrate)

	ts := time.Now().Unix()
	cs.VarDiff.TimestampArr[ts] += ts
	//_ = cs.calcVarDiff(float64(cs.difficulty), s)
	//targetHex := util.GetTargetHex(cs.difficulty)
	//cs.endpoint.targetHex = targetHex
	//cs.difficulty = cs.calcVarDiff(float64(cs.difficulty), s)

	return true, ""
}
