package stratum

import (
	"encoding/hex"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Nelbert442/dero-golang-pool/util"
)

const (
	paramAddr = iota
	paramWID  = iota
	paramPID  = iota
	paramDiff = iota
)

var noncePattern *regexp.Regexp
var HandlersInfoLogger = logFileOutHandlers("INFO")
var HandlersErrorLogger = logFileOutHandlers("ERROR")

func init() {
	noncePattern, _ = regexp.Compile("^[0-9a-f]{8}$")
}

func (s *StratumServer) handleLoginRPC(cs *Session, params *LoginParams) (*JobReply, *ErrorReply) {

	var id string
	// Login validation / splitting optimized by Peppinux (https://github.com/peppinux)
	address, workID, paymentid, fixDiff, isSolo := s.splitLoginString(params.Login)

	// PaymentID Length Validation
	if paymentid != "" {
		if len(paymentid) == 16 || len(paymentid) == 64 {
			_, err := hex.DecodeString(paymentid)

			if err != nil {
				log.Printf("[Handlers] Invalid paymentID %s used for login by %s - %s", paymentid, cs.ip, params.Login)
				HandlersErrorLogger.Printf("[Handlers] Invalid paymentID %s used for login by %s - %s", paymentid, cs.ip, params.Login)
				return nil, &ErrorReply{Code: -1, Message: "Invalid paymentID used for login"}
			}
		} else {
			log.Printf("[Handlers] Invalid paymentID %s used for login by %s - %s", paymentid, cs.ip, params.Login)
			HandlersErrorLogger.Printf("[Handlers] Invalid paymentID %s used for login by %s - %s", paymentid, cs.ip, params.Login)
			return nil, &ErrorReply{Code: -1, Message: "Invalid paymentID used for login"}
		}

		// Adding paymentid onto the worker id because later when payments are processed, it's easily identifiable what is the paymentid to supply for creating tx etc.
		id = address + "+" + paymentid
	}

	// If solo is used, then add solo: to front of id for logging
	if isSolo && s.config.Stratum.SoloMining.Enabled {
		if id != "" {
			// If id is not "" (default value upon var), then it must have a paymentid
			id = "solo" + s.config.Stratum.SoloMining.AddressSeparator + id
		} else {
			id = "solo" + s.config.Stratum.SoloMining.AddressSeparator + address
		}
	}

	// If workID is used, then append with work separator, this will be easily deciphered later for payments. In future, could store id and values separately so that address payout is clearer
	if workID != address && workID != "" {
		if id != "" {
			// If id is not "" (default value upon var), then it must have a paymentid or is solo and has been set. So append workID to it
			id = id + s.config.Stratum.WorkerID.AddressSeparator + workID
		} else {
			// If id is "" (default value upon var), then it does not have paymentid and append workID to address normally
			id = address + s.config.Stratum.WorkerID.AddressSeparator + workID
		}
	} else {
		if id == "" {
			// If id is "" (default value upon var), then it does not have a paymentid and in this else doesn't have workid, so set id to address for default. Otherwise, id has already been set
			id = address
		}
	}

	switch s.config.Coin {
	case "DERO":
		if !util.ValidateAddress(address, s.config.Address) {
			log.Printf("[Handlers] Invalid address %s used for login by %s", address, cs.ip)
			HandlersErrorLogger.Printf("[Handlers] Invalid address %s used for login by %s", address, cs.ip)
			return nil, &ErrorReply{Code: -1, Message: "Invalid address used for login"}
		}
	default:
		if !util.ValidateAddressNonDERO(address, s.config.Address) {
			log.Printf("[Handlers] Invalid address %s used for login by %s", address, cs.ip)
			HandlersErrorLogger.Printf("[Handlers] Invalid address %s used for login by %s", address, cs.ip)
			return nil, &ErrorReply{Code: -1, Message: "Invalid address used for login"}
		}
	}

	t := s.currentBlockTemplate()
	if t == nil {
		return nil, &ErrorReply{Code: -1, Message: "Job not ready"}
	}

	miner, ok := s.miners.Get(id)
	if !ok {
		log.Printf("[Handlers] Registering new miner: %s@%s, Address: %s, PaymentID: %s, fixedDiff: %v, isSolo: %v", id, cs.ip, address, paymentid, fixDiff, isSolo)
		HandlersInfoLogger.Printf("[Handlers] Registering new miner: %s@%s, Address: %s, PaymentID: %s, fixedDiff: %v, isSolo: %v", id, cs.ip, address, paymentid, fixDiff, isSolo)
		miner = NewMiner(id, address, paymentid, fixDiff, workID, isSolo, cs.ip)
		s.registerMiner(miner)
		Graviton_backend.WriteMinerIDRegistration(miner)
	} else {
		now := util.MakeTimestamp() / 1000
		miner.StartedAt = now
	}

	log.Printf("[Handlers] Miner connected %s@%s, Address: %s, PaymentID: %s, fixedDiff: %v, isSolo: %v", id, cs.ip, address, paymentid, fixDiff, isSolo)
	HandlersInfoLogger.Printf("[Handlers] Miner connected %s@%s, Address: %s, PaymentID: %s, fixedDiff: %v, isSolo: %v", id, cs.ip, address, paymentid, fixDiff, isSolo)

	s.registerSession(cs)
	miner.heartbeat()

	// Initially set cs.difficulty. If there's no fixDiff defined, inside of cs.getJob the diff target will be set to cs.endpoint.difficulty,
	// otherwise will be set to fixDiff (as long as it's above min diff in config)
	if fixDiff != 0 {
		cs.difficulty = int64(fixDiff)
		cs.isFixedDiff = true
	} else {
		cs.difficulty = cs.endpoint.config.Difficulty
		cs.isFixedDiff = false
	}

	//log.Printf("[handleGetJobRPC] getJob: %v", cs.getJob(t))
	job := cs.getJob(t, s, 0)
	return &JobReply{Id: id, Job: job, Status: "OK"}, nil
}

func (s *StratumServer) handleGetJobRPC(cs *Session, params *GetJobParams) (*JobReplyData, *ErrorReply) {
	miner, ok := s.miners.Get(params.Id)
	if !ok {
		return nil, &ErrorReply{Code: -1, Message: "Unauthenticated"}
	}
	t := s.currentBlockTemplate()
	if t == nil || s.isSick() {
		return nil, &ErrorReply{Code: -1, Message: "Job not ready"}
	}
	miner.heartbeat()

	reply := cs.getJob(t, s, 0)
	return reply, nil
}

func (s *StratumServer) handleSubmitRPC(cs *Session, params *SubmitParams) (*StatusReply, *ErrorReply) {
	miner, ok := s.miners.Get(params.Id)
	if !ok {
		return nil, &ErrorReply{Code: -1, Message: "Unauthenticated"}
	}
	miner.heartbeat()

	// Upon job submissions, miner(s) will get error message saying to contact pool owner when stratum .isSick()
	if s.isSick() {
		return nil, &ErrorReply{Code: -1, Message: "Server error. Contact pool owner."}
	}

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
		atomic.AddInt64(&miner.InvalidShares, 1)
		return nil, &ErrorReply{Code: -1, Message: "Duplicate share"}
	}

	t := s.currentBlockTemplate()
	if job.height != t.Height {
		log.Printf("[Handlers] Stale share for height %d from %s@%s", job.height, miner.Id, cs.ip)
		HandlersErrorLogger.Printf("[Handlers] Stale share for height %d from %s@%s", job.height, miner.Id, cs.ip)
		atomic.AddInt64(&miner.StaleShares, 1)
		return nil, &ErrorReply{Code: -1, Message: "Block expired"}
	}

	validShare, minerOutput := miner.processShare(s, cs, job, t, nonce, params)
	if !validShare {
		return nil, &ErrorReply{Code: -1, Message: minerOutput}
	}
	return &StatusReply{Status: "OK"}, nil
}

func (s *StratumServer) handleUnknownRPC(req *JSONRpcReq) *ErrorReply {
	log.Printf("[Handlers] Unknown RPC method: %v", req)
	HandlersErrorLogger.Printf("[Handlers] Unknown RPC method: %v", req)
	return &ErrorReply{Code: -1, Message: "Invalid method"}
}

func (s *StratumServer) broadcastNewJobs() {
	t := s.currentBlockTemplate()
	if t == nil || s.isSick() {
		return
	}
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	count := len(s.sessions)
	log.Printf("[Handlers] Broadcasting new jobs to %d miners", count)
	HandlersInfoLogger.Printf("[Handlers] Broadcasting new jobs to %d miners", count)
	bcast := make(chan int, 1024*16)
	n := 0

	for m := range s.sessions {
		n++
		bcast <- n
		go func(cs *Session) {
			reply := cs.getJob(t, s, 0)
			err := cs.pushMessage("job", &reply)

			<-bcast
			if err != nil {
				log.Printf("[Handlers] Job transmit error to %s: %v", cs.ip, err)
				HandlersErrorLogger.Printf("[Handlers] Job transmit error to %s: %v", cs.ip, err)
				s.removeSession(cs)
			} else {
				s.setDeadline(cs.conn)
			}
		}(m)
	}
}

func (s *StratumServer) updateFixedDiffJobs() {
	t := s.currentBlockTemplate()
	if t == nil || s.isSick() {
		return
	}
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	bcast := make(chan int, 1024*16)
	n := 0

	for m := range s.sessions {
		n++
		bcast <- n
		go func(cs *Session) {
			// If fixed diff, ignore cycling update miner jobs
			if !cs.isFixedDiff {
				preJob := cs.difficulty
				newDiff := cs.calcVarDiff(float64(preJob), s)
				// If job diffs aren't the same, advertise new job
				if preJob != newDiff {
					reply := cs.getJob(t, s, newDiff)
					log.Printf("[Handlers] Retargetting difficulty from %v to %v for %v", preJob, newDiff, cs.ip)
					HandlersInfoLogger.Printf("[Handlers] Retargetting difficulty from %v to %v for %v", preJob, newDiff, cs.ip)
					cs.difficulty = newDiff
					err := cs.pushMessage("job", &reply)
					<-bcast
					if err != nil {
						log.Printf("[Handlers] Job transmit error to %s: %v", cs.ip, err)
						HandlersErrorLogger.Printf("[Handlers] Job transmit error to %s: %v", cs.ip, err)
						s.removeSession(cs)
					} else {
						s.setDeadline(cs.conn)
					}
				}
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

// Optimized splitting functions with runes from @Peppinux (https://github.com/peppinux)
func (s *StratumServer) splitLoginString(loginWorkerPair string) (addr, wid, pid string, diff uint64, isSolo bool) {
	currParam := paramAddr // String always starts with ADDRESS
	currSubstr := ""       // Substring starts empty

	// Check for solo:
	soloPair := "solo" + s.config.Stratum.SoloMining.AddressSeparator
	if strings.Index(loginWorkerPair, soloPair) != -1 {
		isSolo = true
		loginWorkerPair = loginWorkerPair[5:len(loginWorkerPair)] // shave off 5 since solo: is 5 chars, but isSolo will return true to be used to append solo: afterwards [retains addr result properly]
		log.Printf("%s", loginWorkerPair)
	} else {
		isSolo = false
	}

	// Since input vals from json are string, need to convert to a rune array, then references just use [0] slice since these are just '@', '+', '.' in config.json
	widAddrSep := []rune(s.config.Stratum.WorkerID.AddressSeparator)
	pidAddrSep := []rune(s.config.Stratum.PaymentID.AddressSeparator)
	fDiffAddrSep := []rune(s.config.Stratum.FixedDiff.AddressSeparator)

	lastPos := len(loginWorkerPair) - 1
	for pos, c := range loginWorkerPair {
		if c != widAddrSep[0] && c != pidAddrSep[0] && c != fDiffAddrSep[0] && pos != lastPos {
			currSubstr += string(c)
		} else {
			if pos == lastPos {
				currSubstr += string(c)
			}

			// Finalize substring
			switch currParam {
			case paramAddr:
				addr = currSubstr
			case paramWID:
				wid = currSubstr
			case paramPID:
				pid = currSubstr
			case paramDiff:
				diff, _ = strconv.ParseUint(currSubstr, 10, 64)
			}

			// Reset substring and find out next param type
			currSubstr = ""
			switch c {
			case widAddrSep[0]:
				currParam = paramWID
			case pidAddrSep[0]:
				currParam = paramPID
			case fDiffAddrSep[0]:
				currParam = paramDiff
			}
		}
	}
	return
}

func logFileOutHandlers(lType string) *log.Logger {
	var logFileName string
	if lType == "ERROR" {
		logFileName = "logs/handlersError.log"
	} else {
		logFileName = "logs/handlers.log"
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
