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
	lastBeat      int64
	startedAt     int64
	validShares   int64
	invalidShares int64
	staleShares   int64
	trustedShares int64
	accepts       int64
	rejects       int64
	shares        map[int64]int64
	roundShares   map[int64]int64
	sync.RWMutex
	id        string
	address   string
	paymentID string
	fixedDiff uint64
	ip        string
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

func NewMiner(id string, address string, paymentid string, fixedDiff uint64, ip string) *Miner {
	shares := make(map[int64]int64)
	return &Miner{id: id, address: address, paymentID: paymentid, fixedDiff: fixedDiff, ip: ip, shares: shares}
}

func (cs *Session) getJob(t *BlockTemplate) *JobReplyData {
	lastBlockHeight := cs.lastBlockHeight
	if lastBlockHeight == t.Height {
		return &JobReplyData{}
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
	reply := &JobReplyData{JobId: job.id, Blob: blob, Target: cs.endpoint.targetHex}
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
	now := util.MakeTimestamp()
	atomic.StoreInt64(&m.lastBeat, now)
}

func (m *Miner) getLastBeat() int64 {
	return atomic.LoadInt64(&m.lastBeat)
}

func (m *Miner) storeShare(diff int64) {
	now := util.MakeTimestamp() / 1000
	m.Lock()
	m.shares[now] += diff
	m.Unlock()
}

// TODO: This needs rework, I don't think it's correct.
func (m *Miner) hashrate(estimationWindow time.Duration) float64 {
	now := util.MakeTimestamp() / 1000
	totalShares := int64(0)
	window := int64(estimationWindow / time.Second)
	boundary := now - m.startedAt

	if boundary > window {
		boundary = window
	}

	m.Lock()
	for k, v := range m.shares {
		if k < now-86400 {
			delete(m.shares, k)
		} else if k >= now-window {
			totalShares += v
		}
	}
	m.Unlock()
	return float64(totalShares) / float64(boundary)
}

func (m *Miner) processShare(s *StratumServer, cs *Session, job *Job, t *BlockTemplate, nonce string, params *SubmitParams) (bool, string) {

	// Var definitions
	checkPowHashBig := false
	success := false
	var result string = params.Result
	var shareType string
	var hashBytes []byte
	var diff big.Int
	diff.SetUint64(t.Difficulty)
	r := s.rpc()

	shareBuff := make([]byte, len(t.Buffer))
	copy(shareBuff, t.Buffer)
	copy(shareBuff[t.Reserved_Offset+4:t.Reserved_Offset+7], cs.endpoint.instanceId)

	extraBuff := new(bytes.Buffer)
	binary.Write(extraBuff, binary.BigEndian, job.extraNonce)
	copy(shareBuff[t.Reserved_Offset:], extraBuff.Bytes())

	nonceBuff, _ := hex.DecodeString(nonce)
	copy(shareBuff[39:], nonceBuff)

	if m.trustedShares >= s.trustedSharesCount {
		shareType = "Trusted"
	} else {
		shareType = "Valid"
	}

	hashBytes, _ = hex.DecodeString(result)

	if s.config.BypassShareValidation || shareType == "Trusted" {
		checkPowHashBig = true
	} else {
		switch s.algo {
		case "astrobwt":
			checkPowHashBig, success = util.AstroBWTHash(shareBuff, diff)

			if !success {
				minerOutput := "Incorrect PoW - if you see often, check input on miner software"
				log.Printf("Bad hash from miner %v@%v", m.id, cs.ip)

				if shareType == "Trusted" {
					log.Printf("[No Trust] Miner is no longer submitting trusted shares: %v@%v", m.id, cs.ip)
					shareType = "Valid"
				}

				atomic.AddInt64(&m.invalidShares, 1)
				atomic.StoreInt64(&m.trustedShares, 0)
				return false, minerOutput
			}

			atomic.AddInt64(&m.trustedShares, 1)

		default:
			// Handle when no algo is defined or unhandled algo is defined, let miner know issues (properly gets sent back in job detail rejection message)
			minerOutput := "Rejected share, no pool algo defined. Contact pool owner."
			log.Printf("Rejected share, no pool algo defined (%s). Contact pool owner - from %v@%v", s.algo, m.id, cs.ip)
			return false, minerOutput
		}
	}

	hashDiff, ok := util.GetHashDifficulty(hashBytes)
	if !ok {
		minerOutput := "Bad hash"
		log.Printf("Bad hash from miner (l197) %v@%v", m.id, cs.ip)
		atomic.AddInt64(&m.invalidShares, 1)
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
			atomic.AddInt64(&m.rejects, 1)
			atomic.AddInt64(&r.Rejects, 1)
			log.Printf("Block rejected at height %d: %v", t.Height, err)
		} else {
			now := util.MakeTimestamp()
			roundShares := atomic.SwapInt64(&s.roundShares, 0)
			ratio := float64(roundShares) / float64(int64(t.Difficulty))
			s.blocksMu.Lock()
			s.blockStats[now] = blockEntry{height: int64(t.Height), hash: blockSubmitReply.BLID, variance: ratio}
			s.blocksMu.Unlock()
			atomic.AddInt64(&m.accepts, 1)
			atomic.AddInt64(&r.Accepts, 1)
			atomic.StoreInt64(&r.LastSubmissionAt, now)
			log.Printf("Block found at height %d, diff: %v, blid: %s, by miner: %v@%v, ratio: %.4f", t.Height, t.Difficulty, blockSubmitReply.BLID, m.id, cs.ip, ratio)

			// Immediately refresh current BT and send new jobs
			s.refreshBlockTemplate(true)

			// Redis store of successful block
			_, err := s.backend.WriteBlock(params.Id, params.JobId, params, cs.endpoint.config.Difficulty, int64(t.Difficulty), int64(t.Height), s.hashrateExpiration, 0, blockSubmitReply.BLID)
			if err != nil {
				log.Println("Failed to insert block data into backend:", err)
			}
		}
	} else if hashDiff.Cmp(cs.endpoint.difficulty) < 0 {
		minerOutput := "Low difficulty share"
		log.Printf("Rejected low difficulty share of %v from %v@%v", hashDiff, m.id, cs.ip)
		atomic.AddInt64(&m.invalidShares, 1)
		return false, minerOutput
	}

	atomic.AddInt64(&s.roundShares, cs.endpoint.config.Difficulty)
	atomic.AddInt64(&m.validShares, 1)
	m.storeShare(cs.endpoint.config.Difficulty)

	// Redis store of successful share
	_, err := s.backend.WriteShare(params.Id, params.JobId, params, cs.endpoint.config.Difficulty, int64(t.Height), s.hashrateExpiration)
	if err != nil {
		log.Println("Failed to insert share data into backend:", err)
	}

	log.Printf("%s share at difficulty %v/%v from %v@%v", shareType, cs.endpoint.config.Difficulty, hashDiff, params.Id, cs.ip)
	return true, ""
}
