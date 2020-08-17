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

	"github.com/deroproject/derosuite/astrobwt"
	"github.com/deroproject/derosuite/blockchain"
	"github.com/deroproject/derosuite/crypto"
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
	accepts       int64
	rejects       int64
	shares        map[int64]int64
	sync.RWMutex
	id string
	ip string
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

func NewMiner(id string, ip string) *Miner {
	shares := make(map[int64]int64)
	return &Miner{id: id, ip: ip, shares: shares}
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

func (m *Miner) processShare(s *StratumServer, cs *Session, job *Job, t *BlockTemplate, nonce string, result string) (bool, string) {
	r := s.rpc()

	var diff big.Int
	diff.SetUint64(t.Difficulty)

	shareBuff := make([]byte, len(t.Buffer))
	copy(shareBuff, t.Buffer)
	copy(shareBuff[t.Reserved_Offset+4:t.Reserved_Offset+7], cs.endpoint.instanceId)

	extraBuff := new(bytes.Buffer)
	binary.Write(extraBuff, binary.BigEndian, job.extraNonce)
	copy(shareBuff[t.Reserved_Offset:], extraBuff.Bytes())

	nonceBuff, _ := hex.DecodeString(nonce)
	copy(shareBuff[39:], nonceBuff)

	var hashBytes []byte
	var powhash crypto.Hash
	var data astrobwt.Data
	var max_pow_size int = 819200 //astrobwt.MAX_LENGTH

	if s.config.BypassShareValidation {
		hashBytes, _ = hex.DecodeString(result)
		copy(powhash[:], hashBytes)
	} else {
		hashBytes, _ = hex.DecodeString(result)
		//hashBytes = astrobwt_optimized.POW_optimized_v2(shareBuff, max_pow_size)

		hash, success := astrobwt.POW_optimized_v2(shareBuff, max_pow_size, &data)
		if !success {
			minerOutput := "Incorrect PoW"
			log.Printf("Bad hash from miner (l171) %v@%v", m.id, cs.ip)
			atomic.AddInt64(&m.invalidShares, 1)
			return false, minerOutput
		}

		//hashBytes, _ = hex.DecodeString(shareBuff)
		copy(powhash[:], hash[:])
	}

	if !s.config.BypassShareValidation && hex.EncodeToString(hashBytes) != result {
		minerOutput := "Bad hash"
		log.Printf("Bad hash from miner (l182) %v@%v", m.id, cs.ip)
		atomic.AddInt64(&m.invalidShares, 1)
		return false, minerOutput
	}

	hashDiff, ok := util.GetHashDifficulty(hashBytes)
	if !ok {
		minerOutput := "Bad hash"
		log.Printf("Bad hash from miner (l190) %v@%v", m.id, cs.ip)
		atomic.AddInt64(&m.invalidShares, 1)
		return false, minerOutput
	}

	block := hashDiff.Cmp(&diff) >= 0

	//if block {
	if blockchain.CheckPowHashBig(powhash, &diff) == true && block {
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
	log.Printf("Valid share at difficulty %v/%v", cs.endpoint.config.Difficulty, hashDiff)
	return true, ""
}
