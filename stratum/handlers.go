package stratum

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync/atomic"

	"git.dero.io/Nelbert442/dero-golang-pool/util"
)

var noncePattern *regexp.Regexp

const defaultWorkerID = ""
const defaultPaymentID = ""
const defaultFixedDiff = ""

func init() {
	noncePattern, _ = regexp.Compile("^[0-9a-f]{8}$")
}

func (s *StratumServer) handleLoginRPC(cs *Session, params *LoginParams) (*JobReply, *ErrorReply) {
	//address, workerID, fixedDiff, paymentID := s.extractWorkerID(params.Login)
	//id := workerID

	address, paymentid, workID, fixDiff := s.extractWorkerID(params.Login)
	var id string
	if workID != address && workID != "" {
		id = address + "~" + workID
	} else {
		id = address
	}
	//fmt.Printf("%+v\n", params)
	if !s.config.BypassAddressValidation && !util.ValidateAddress(address, s.config.Address) {
		log.Printf("Invalid address %s used for login by %s", address, cs.ip)
		return nil, &ErrorReply{Code: -1, Message: "Invalid address used for login"}
	}

	t := s.currentBlockTemplate()
	if t == nil {
		return nil, &ErrorReply{Code: -1, Message: "Job not ready"}
	}

	miner, ok := s.miners.Get(id)
	if !ok {
		log.Printf("Registering new miner: %s@%s, Address: %s, PaymentID: %s, fixedDiff: %s", id, cs.ip, address, paymentid, fixDiff)
		miner = NewMiner(id, address, paymentid, fixDiff, cs.ip)
		s.registerMiner(miner)
	}

	log.Printf("Miner connected %s@%s, Address: %s, PaymentID: %s, fixedDiff: %s", id, cs.ip, address, paymentid, fixDiff)

	s.registerSession(cs)
	miner.heartbeat()

	return &JobReply{Id: id, Job: cs.getJob(t), Status: "OK"}, nil
}

func (s *StratumServer) handleGetJobRPC(cs *Session, params *GetJobParams) (*JobReplyData, *ErrorReply) {
	miner, ok := s.miners.Get(params.Id)
	if !ok {
		return nil, &ErrorReply{Code: -1, Message: "Unauthenticated"}
	}
	t := s.currentBlockTemplate()
	if t == nil {
		return nil, &ErrorReply{Code: -1, Message: "Job not ready"}
	}
	miner.heartbeat()
	return cs.getJob(t), nil
}

func (s *StratumServer) handleSubmitRPC(cs *Session, params *SubmitParams) (*StatusReply, *ErrorReply) {
	miner, ok := s.miners.Get(params.Id)
	if !ok {
		return nil, &ErrorReply{Code: -1, Message: "Unauthenticated"}
	}
	miner.heartbeat()

	job := cs.findJob(params.JobId)
	if job == nil {
		return nil, &ErrorReply{Code: -1, Message: "Invalid job id"}
	}

	if !noncePattern.MatchString(params.Nonce) {
		return nil, &ErrorReply{Code: -1, Message: "Malformed nonce"}
	}
	nonce := strings.ToLower(params.Nonce)
	exist := job.submit(nonce)
	if exist {
		atomic.AddInt64(&miner.invalidShares, 1)
		return nil, &ErrorReply{Code: -1, Message: "Duplicate share"}
	}

	t := s.currentBlockTemplate()
	if job.height != t.Height {
		log.Printf("Stale share for height %d from %s@%s", job.height, miner.id, cs.ip)
		atomic.AddInt64(&miner.staleShares, 1)
		return nil, &ErrorReply{Code: -1, Message: "Block expired"}
	}

	validShare, minerOutput := miner.processShare(s, cs, job, t, nonce, params)
	if !validShare {
		return nil, &ErrorReply{Code: -1, Message: minerOutput}
	}
	return &StatusReply{Status: "OK"}, nil
}

func (s *StratumServer) handleUnknownRPC(req *JSONRpcReq) *ErrorReply {
	log.Printf("Unknown RPC method: %v", req)
	return &ErrorReply{Code: -1, Message: "Invalid method"}
}

func (s *StratumServer) broadcastNewJobs() {
	t := s.currentBlockTemplate()
	if t == nil {
		return
	}
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	count := len(s.sessions)
	log.Printf("Broadcasting new jobs to %d miners", count)
	bcast := make(chan int, 1024*16)
	n := 0

	for m := range s.sessions {
		n++
		bcast <- n
		go func(cs *Session) {
			reply := cs.getJob(t)
			err := cs.pushMessage("job", &reply)
			fmt.Printf("[Job Broadcast] %+v\n", reply)
			<-bcast
			if err != nil {
				log.Printf("Job transmit error to %s: %v", cs.ip, err)
				s.removeSession(cs)
			} else {
				s.setDeadline(cs.conn)
			}
		}(m)
	}
}

func (s *StratumServer) refreshBlockTemplate(bcast bool) {
	newBlock := s.fetchBlockTemplate()
	if newBlock && bcast {
		s.broadcastNewJobs()
	}
}

func (s *StratumServer) extractWorkerID(loginWorkerPair string) (string, string, string, string) { //, string, string) {
	// Stratum --> workerID --> addressSeparator in config.json
	var addr, payID, workID, fixDiff string
	// This sets up a little logic (probably not perfect, optimize later) to split out the different login options (paymentID, fixedDiff [future], workerID, address)
	if strings.ContainsAny(loginWorkerPair, s.config.Stratum.WorkerID.AddressSeparator+s.config.Stratum.PaymentID.AddressSeparator+s.config.Stratum.FixedDiff.AddressSeparator) {
		// Split loginWorkerPair by workerID separator
		split1 := strings.SplitN(loginWorkerPair, s.config.Stratum.WorkerID.AddressSeparator, 2)

		if strings.ContainsAny(split1[0], s.config.Stratum.PaymentID.AddressSeparator) {
			// Split loginworkerapi[0] by paymentid
			split2 := strings.SplitN(split1[0], s.config.Stratum.PaymentID.AddressSeparator, 2)

			addr = split2[0]
			payID = split2[1]
		} else {
			addr = split1[0]
		}

		if strings.ContainsAny(split1[1], s.config.Stratum.PaymentID.AddressSeparator) {
			// Split loginworkerapi[1] by paymentid
			split3 := strings.SplitN(split1[1], s.config.Stratum.PaymentID.AddressSeparator, 2)

			workID = split3[0]
			payID = split3[1]
		} else {
			workID = split1[1]
		}
	} else {
		return loginWorkerPair, defaultPaymentID, defaultWorkerID, defaultFixedDiff
	}
	/*parts := strings.SplitN(loginWorkerPair, s.config.Stratum.WorkerID.AddressSeparator, 2)
	if len(parts) > 1 {
		return parts[0], parts[1]
	}*/

	return addr, payID, workID, fixDiff
}
