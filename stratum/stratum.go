package stratum

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/rpc"
	"git.dero.io/Nelbert442/dero-golang-pool/util"
)

type StratumServer struct {
	luckWindow         int64
	largeLuckWindow    int64
	roundShares        int64
	blockStats         map[int64]blockEntry
	config             *pool.Config
	miners             MinersMap
	blockTemplate      atomic.Value
	upstream           int32
	upstreams          []*rpc.RPCClient
	timeout            time.Duration
	estimationWindow   time.Duration
	blocksMu           sync.RWMutex
	sessionsMu         sync.RWMutex
	sessions           map[*Session]struct{}
	algo               string
	trustedSharesCount int64
	backend            *RedisClient
	hashrateExpiration time.Duration
	failsCount         int64
}

type blockEntry struct {
	height   int64
	variance float64
	hash     string
}

type Endpoint struct {
	jobSequence uint64
	config      *pool.Port
	difficulty  *big.Int
	instanceId  []byte
	extraNonce  uint32
	targetHex   string
}

type VarDiff struct {
	config     *pool.VarDiff
	difficulty int64
	average    float64
	sinceLast  time.Time
}

type Session struct {
	lastBlockHeight uint64
	sync.Mutex
	conn        *net.TCPConn
	enc         *json.Encoder
	ip          string
	endpoint    *Endpoint
	validJobs   []*Job
	difficulty  int64
	varDiff     *VarDiff
	isFixedDiff bool
}

const (
	MaxReqSize = 10 * 1024
)

func NewStratum(cfg *pool.Config) *StratumServer {
	stratum := &StratumServer{config: cfg, blockStats: make(map[int64]blockEntry)}

	// Create new redis client/connection
	if cfg.Redis.Enabled {
		backend := NewRedisClient(&cfg.Redis, cfg.Coin)
		stratum.backend = backend
	}

	// Set stratum.upstreams length based on cfg.Upstream only if they are set enabled: true. We use arr to simulate this and filter out cfg.Upstream objects
	var arr []pool.Upstream
	for _, f := range cfg.Upstream {
		if f.Enabled {
			arr = append(arr, f)
		}
	}

	stratum.upstreams = make([]*rpc.RPCClient, len(arr))
	for i, v := range arr {
		client, err := rpc.NewRPCClient(&v)
		if err != nil {
			log.Fatal(err)
		} else {
			stratum.upstreams[i] = client
			log.Printf("Upstream: %s => %s", client.Name, client.Url)
		}
	}
	log.Printf("Default upstream: %s => %s", stratum.rpc().Name, stratum.rpc().Url)

	stratum.miners = NewMinersMap()
	stratum.sessions = make(map[*Session]struct{})
	stratum.algo = cfg.Algo
	stratum.trustedSharesCount = cfg.TrustedSharesCount

	timeout, _ := time.ParseDuration(cfg.Stratum.Timeout)
	stratum.timeout = timeout

	refreshIntv, _ := time.ParseDuration(cfg.BlockRefreshInterval)
	refreshTimer := time.NewTimer(refreshIntv)
	log.Printf("Set block refresh every %v", refreshIntv)

	hashExpiration, _ := time.ParseDuration(cfg.HashrateExpiration)
	stratum.hashrateExpiration = hashExpiration

	checkIntv, _ := time.ParseDuration(cfg.UpstreamCheckInterval)
	checkTimer := time.NewTimer(checkIntv)
	log.Printf("Set upstream check interval every %v", refreshIntv)

	infoIntv, _ := time.ParseDuration(cfg.UpstreamCheckInterval)
	infoTimer := time.NewTimer(infoIntv)

	// Init block template
	go stratum.refreshBlockTemplate(false)

	go func() {
		for {
			select {
			case <-refreshTimer.C:
				stratum.refreshBlockTemplate(true)
				refreshTimer.Reset(refreshIntv)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-checkTimer.C:
				stratum.checkUpstreams()
				checkTimer.Reset(checkIntv)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-infoTimer.C:
				currentWork := stratum.currentWork()
				poll := func(v *rpc.RPCClient) {
					// Need to make sure that this isn't too heavy of action, to call GetBlockByHash here. Otherwise, need to store it in another fashion
					var diff big.Int
					diff.SetUint64(currentWork.Difficulty)

					prevBlock, getHashERR := v.GetBlockByHash(currentWork.Prev_Hash)
					lastBlock := prevBlock.BlockHeader

					if getHashERR != nil {
						log.Printf("Error while retrieving block %s from node: %v", currentWork.Prev_Hash, getHashERR)
					}

					writeLBErr := stratum.backend.WriteLastBlockState(cfg.Coin, lastBlock.Difficulty, lastBlock.Height, int64(lastBlock.Timestamp), int64(lastBlock.Reward), lastBlock.Hash)
					err2 := stratum.backend.WriteNodeState(cfg.Coin, int64(currentWork.Height), &diff)

					_, err := v.UpdateInfo()
					if err != nil || err2 != nil || writeLBErr != nil {
						log.Printf("Unable to update info on upstream %s: %v", v.Name, err)
						stratum.markSick()
					} else {
						stratum.markOk()
					}
				}
				current := stratum.rpc()
				poll(current)

				// Async rpc call to not block on rpc timeout, ignoring current
				go func() {
					for _, v := range stratum.upstreams {
						if v != current {
							poll(v)
						}
					}
				}()
				infoTimer.Reset(infoIntv)
			}
		}
	}()

	return stratum
}

// Defines parameters for the ports to be listened on, such as default difficulty
func NewEndpoint(cfg *pool.Port) *Endpoint {
	e := &Endpoint{config: cfg}
	e.instanceId = make([]byte, 4)
	_, err := rand.Read(e.instanceId)
	if err != nil {
		log.Fatalf("Can't seed with random bytes: %v", err)
	}
	//log.Printf("[NewEndpoint] e.config.Difficulty: %v", e.config.Difficulty)
	e.targetHex = util.GetTargetHex(e.config.Difficulty)
	e.difficulty = big.NewInt(e.config.Difficulty)
	return e
}

// Sets up stratum to listen on the ports in config.json
func (s *StratumServer) Listen() {
	quit := make(chan bool)
	for _, port := range s.config.Stratum.Ports {
		go func(cfg pool.Port) {
			e := NewEndpoint(&cfg)
			//log.Printf("[s.Listen] NewEndpoint: %v", cfg)
			e.Listen(s)
		}(port)
	}
	<-quit
}

// Starts listening on the ports defined in s.Listen()
func (e *Endpoint) Listen(s *StratumServer) {
	bindAddr := fmt.Sprintf("%s:%d", e.config.Host, e.config.Port)
	addr, err := net.ResolveTCPAddr("tcp", bindAddr)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	server, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	defer server.Close()

	log.Printf("Stratum listening on %s", bindAddr)
	accept := make(chan int, e.config.MaxConn)
	n := 0

	for {
		conn, err := server.AcceptTCP()
		if err != nil {
			continue
		}
		conn.SetKeepAlive(true)
		ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		cs := &Session{conn: conn, ip: ip, enc: json.NewEncoder(conn), endpoint: e}
		//log.Printf("[e.Listen] Listen: %v", cs)
		n++

		accept <- n
		go func() {
			s.handleClient(cs, e)
			<-accept
		}()
	}
}

// Handles inbound client data, and sends off to handleMessage for processing things like login, submits etc.
func (s *StratumServer) handleClient(cs *Session, e *Endpoint) {
	connbuff := bufio.NewReaderSize(cs.conn, MaxReqSize)
	s.setDeadline(cs.conn)

	for {
		data, isPrefix, err := connbuff.ReadLine()
		if isPrefix {
			log.Println("Socket flood detected from", cs.ip)
			break
		} else if err == io.EOF {
			log.Println("Client disconnected", cs.ip)
			break
		} else if err != nil {
			log.Println("Error reading:", err)
			break
		}

		// NOTICE: cpuminer-multi sends junk newlines, so we demand at least 1 byte for decode
		// NOTICE: Ns*CNMiner.exe will send malformed JSON on very low diff, not sure we should handle this
		if len(data) > 1 {
			var req JSONRpcReq
			err = json.Unmarshal(data, &req)
			if err != nil {
				log.Printf("Malformed request from %s: %v", cs.ip, err)
				break
			}
			s.setDeadline(cs.conn)
			//log.Printf("[s.handleClient] handleMessage: s: %v, e: %v, req: %v", s, e, req)
			err = cs.handleMessage(s, e, &req)
			if err != nil {
				break
			}
		}
	}
	s.removeSession(cs)
	cs.conn.Close()
}

// Handle messages , login and submit are common
func (cs *Session) handleMessage(s *StratumServer, e *Endpoint, req *JSONRpcReq) error {
	if req.Id == nil {
		err := fmt.Errorf("Server disconnect request")
		log.Println(err)
		return err
	} else if req.Params == nil {
		err := fmt.Errorf("Server RPC request params")
		log.Println(err)
		return err
	}

	// Handle RPC methods
	switch req.Method {

	case "login":
		var params LoginParams

		err := json.Unmarshal(*req.Params, &params)
		//fmt.Printf("[login] %+v\n", params)
		if err != nil {
			log.Println("Unable to parse params")
			return err
		}
		reply, errReply := s.handleLoginRPC(cs, &params)
		if errReply != nil {
			return cs.sendError(req.Id, errReply, true)
		}
		return cs.sendResult(req.Id, &reply)
	case "getjob":
		var params GetJobParams
		err := json.Unmarshal(*req.Params, &params)
		//fmt.Printf("[getJob] %+v\n", params)
		if err != nil {
			log.Println("Unable to parse params")
			return err
		}
		reply, errReply := s.handleGetJobRPC(cs, &params)
		if errReply != nil {
			return cs.sendError(req.Id, errReply, true)
		}
		return cs.sendResult(req.Id, &reply)
	case "submit":
		var params SubmitParams
		err := json.Unmarshal(*req.Params, &params)
		//fmt.Printf("[submit] %+v\n", params)
		if err != nil {
			log.Println("Unable to parse params")
			return err
		}
		reply, errReply := s.handleSubmitRPC(cs, &params)
		if errReply != nil {
			return cs.sendError(req.Id, errReply, false)
		}
		return cs.sendResult(req.Id, &reply)
	case "keepalived":
		return cs.sendResult(req.Id, &StatusReply{Status: "KEEPALIVED"})
	default:
		errReply := s.handleUnknownRPC(req)
		return cs.sendError(req.Id, errReply, true)
	}
}

func (cs *Session) sendResult(id *json.RawMessage, result interface{}) error {
	cs.Lock()
	defer cs.Unlock()
	message := JSONRpcResp{Id: id, Version: "2.0", Error: nil, Result: result}
	return cs.enc.Encode(&message)
}

func (cs *Session) pushMessage(method string, params interface{}) error {
	cs.Lock()
	defer cs.Unlock()
	message := JSONPushMessage{Version: "2.0", Method: method, Params: params}
	return cs.enc.Encode(&message)
}

func (cs *Session) sendError(id *json.RawMessage, reply *ErrorReply, drop bool) error {
	cs.Lock()
	defer cs.Unlock()
	message := JSONRpcResp{Id: id, Version: "2.0", Error: reply}
	err := cs.enc.Encode(&message)
	if err != nil {
		return err
	}
	if drop {
		return fmt.Errorf("Server disconnect request")
	}
	return nil
}

func (s *StratumServer) setDeadline(conn *net.TCPConn) {
	conn.SetDeadline(time.Now().Add(s.timeout))
}

func (s *StratumServer) registerSession(cs *Session) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	s.sessions[cs] = struct{}{}
}

func (s *StratumServer) removeSession(cs *Session) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	delete(s.sessions, cs)
}

/*
// Unused atm
func (s *StratumServer) isActive(cs *Session) bool {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	_, exist := s.sessions[cs]
	return exist
}*/

func (s *StratumServer) registerMiner(miner *Miner) {
	s.miners.Set(miner.id, miner)
}

/*
// Unused atm
func (s *StratumServer) removeMiner(id string) {
	s.miners.Remove(id)
}*/

func (s *StratumServer) currentBlockTemplate() *BlockTemplate {
	if t := s.blockTemplate.Load(); t != nil {
		return t.(*BlockTemplate)
	}
	return nil
}

func (s *StratumServer) currentWork() *BlockTemplate {
	work := s.blockTemplate.Load()
	if work != nil {
		return work.(*BlockTemplate)
	} else {
		return nil
	}
}

// Poll upstreams for health status
func (s *StratumServer) checkUpstreams() {
	candidate := int32(0)
	backup := false

	for i, v := range s.upstreams {
		ok, err := v.Check(10, s.config.Address)
		if err != nil {
			log.Printf("Upstream %v didn't pass check: %v", v.Name, err)
		}
		if ok && !backup {
			candidate = int32(i)
			backup = true
		}
	}

	if s.upstream != candidate {
		log.Printf("Switching to %v upstream", s.upstreams[candidate].Name)
		atomic.StoreInt32(&s.upstream, candidate)
	}
}

// Loads the current active upstream that is used for getting blocks etc.
func (s *StratumServer) rpc() *rpc.RPCClient {
	i := atomic.LoadInt32(&s.upstream)
	return s.upstreams[i]
}

// Mark the stratum server sick if it fails to connect to redis
func (s *StratumServer) markSick() {
	atomic.AddInt64(&s.failsCount, 1)
}

// Checks if the stratum server is sick based on failsCount and if healthcheck is true, to see if >= maxfails from config.json
func (s *StratumServer) isSick() bool {
	x := atomic.LoadInt64(&s.failsCount)
	if s.config.Stratum.HealthCheck && x >= s.config.Stratum.MaxFails {
		return true
	}
	return false
}

// Upon success to redis, set failsCount to 0 and mark OK again
func (s *StratumServer) markOk() {
	atomic.StoreInt64(&s.failsCount, 0)
}
