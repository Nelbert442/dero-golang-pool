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
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/rpc"
	"git.dero.io/Nelbert442/dero-golang-pool/util"
)

type StratumServer struct {
	roundShares        int64
	config             *pool.Config
	miners             MinersMap
	blockTemplate      atomic.Value
	upstream           int32
	upstreams          []*rpc.RPCClient
	timeout            time.Duration
	estimationWindow   time.Duration
	sessionsMu         sync.RWMutex
	sessions           map[*Session]struct{}
	algo               string
	trustedSharesCount int64
	gravitonDB         *GravitonStore
	hashrateExpiration time.Duration
	failsCount         int64
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
	Difficulty            int64
	Average               float64
	TimestampArr          map[int64]int64
	LastRetargetTimestamp int64
	LastTimeStamp         int64
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
	VarDiff     *VarDiff
	isFixedDiff bool
}

const (
	MaxReqSize = 10 * 1024
)

func NewStratum(cfg *pool.Config) *StratumServer {
	stratum := &StratumServer{config: cfg}

	// Setup our Ctrl+C handler
	stratum.SetupCloseHandler()

	// Startup/create new gravitondb (if it doesn't exist), write the configuration file (config.json) into storage for use / api surfacing later
	//stratum.gravitonDB = Graviton_backend
	Graviton_backend.NewGravDB(cfg.PoolHost, "pooldb") //stratum.gravitonDB.NewGravDB(cfg.PoolHost, "pooldb") // TODO: Add to params in config.json file
	Graviton_backend.WriteConfig(cfg)                  //stratum.gravitonDB.WriteConfig(cfg)

	/* - testing just to output a val from db while development process is going
	plConfig := stratum.gravitonDB.GetConfig(cfg.Coin)
	if plConfig != nil {
		log.Printf("Config: %v", plConfig)
	}

	blocks := stratum.gravitonDB.GetBlocksFound()
	for _, value := range blocks.MinedBlocks {
		log.Printf("Blocks found: %v", value)
	}

	paymentsProcessed := stratum.gravitonDB.GetProcessedPayments()
	for _, value := range paymentsProcessed.MinerPayments {
		log.Printf("Payments processed: %v", value)
	}

	paymentsPending := stratum.gravitonDB.GetPendingPayments()
	for _, value := range paymentsPending {
		log.Printf("Payments pending: %v", value)
	}

	blocksFound := stratum.gravitonDB.GetBlocksFound("matured")
	for _, value := range blocksFound.MinedBlocks {
		log.Printf("Matured blocks: %v", value)
	}

	minerStats := stratum.gravitonDB.GetMinerIDRegistrations()
	log.Printf("Miner: %v", minerStats)
	*/

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
			log.Printf("[Stratum] Upstream: %s => %s", client.Name, client.Url)
		}
	}
	log.Printf("[Stratum] Default upstream: %s => %s", stratum.rpc().Name, stratum.rpc().Url)

	stratum.miners = NewMinersMap()
	stratum.sessions = make(map[*Session]struct{})
	stratum.algo = cfg.Algo
	stratum.trustedSharesCount = cfg.TrustedSharesCount

	timeout, _ := time.ParseDuration(cfg.Stratum.Timeout)
	stratum.timeout = timeout

	refreshIntv, _ := time.ParseDuration(cfg.BlockRefreshInterval)
	refreshTimer := time.NewTimer(refreshIntv)
	log.Printf("[Stratum] Set block refresh every %v", refreshIntv)

	hashExpiration, _ := time.ParseDuration(cfg.HashrateExpiration)
	stratum.hashrateExpiration = hashExpiration

	hashWindow, _ := time.ParseDuration(cfg.API.HashrateWindow)
	stratum.estimationWindow = hashWindow

	checkIntv, _ := time.ParseDuration(cfg.UpstreamCheckInterval)
	checkTimer := time.NewTimer(checkIntv)
	log.Printf("[Stratum] Set upstream check interval every %v", checkIntv)

	minerStatsIntv, _ := time.ParseDuration("2s")
	minerStatsTimer := time.NewTimer(minerStatsIntv)
	log.Printf("[Stratum] Set upstream check interval every %v", checkIntv)

	infoIntv, _ := time.ParseDuration(cfg.UpstreamCheckInterval)
	infoTimer := time.NewTimer(infoIntv)
	// TODO: Separate out individual config intervals for miner stats + lastblock stats
	log.Printf("[Stratum] Set miner stats store interval every %v", infoIntv)
	log.Printf("[Stratum] Set lastblock stats store interval every %v", infoIntv)

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
			case <-checkTimer.C:
				stratum.updateFixedDiffJobs()
				checkTimer.Reset(checkIntv)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-minerStatsTimer.C:
				// Write miner stats
				err := Graviton_backend.WriteMinerStats(stratum.miners, stratum.hashrateExpiration)
				if err != nil {
					log.Printf("[Stratum] Err storing miner stats: %v", err)
				}
				minerStatsTimer.Reset(minerStatsIntv)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-infoTimer.C:
				currentWork := stratum.currentWork()
				poll := func(v *rpc.RPCClient) {
					// Need to make sure that this isn't too heavy of action, to call GetLastBlockHeader here. Otherwise, need to store it in another fashion
					var diff big.Int
					diff.SetUint64(currentWork.Difficulty)

					prevBlock, getHashERR := v.GetLastBlockHeader()

					if getHashERR != nil {
						log.Printf("[Stratum] Error while retrieving block %s from node: %v", currentWork.Prev_Hash, getHashERR)
					} else {
						lastBlock := prevBlock.BlockHeader
						lastblockDB := &LastBlock{Difficulty: lastBlock.Difficulty, Height: lastBlock.Height, Timestamp: int64(lastBlock.Timestamp), Reward: int64(lastBlock.Reward), Hash: lastBlock.Hash}
						lastblockErr := Graviton_backend.WriteLastBlock(lastblockDB) //stratum.gravitonDB.WriteLastBlock(lastblockDB)
						if lastblockErr != nil {
							log.Printf("[Stratum] Graviton DB err: %v", lastblockErr)
						}
					}

					_, err := v.UpdateInfo()
					if err != nil {
						log.Printf("[Stratum] Unable to update info on upstream %s: %v", v.Name, err)
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
		log.Fatalf("[Stratum] Can't seed with random bytes: %v", err)
	}
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
		log.Fatalf("[Stratum] Error: %v", err)
	}
	server, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatalf("[Stratum] Error: %v", err)
	}
	defer server.Close()

	log.Printf("[Stratum] Stratum listening on %s", bindAddr)
	accept := make(chan int, e.config.MaxConn)
	n := 0

	for {
		conn, err := server.AcceptTCP()
		if err != nil {
			continue
		}
		conn.SetKeepAlive(true)
		ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())

		VarDiff := &VarDiff{}

		cs := &Session{conn: conn, ip: ip, enc: json.NewEncoder(conn), endpoint: e, VarDiff: VarDiff}
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
			log.Println("[Stratum] Socket flood detected from", cs.ip)
			break
		} else if err == io.EOF {
			log.Println("[Stratum] Client disconnected", cs.ip)
			break
		} else if err != nil {
			log.Println("[Stratum] Error reading:", err)
			break
		}

		// NOTICE: cpuminer-multi sends junk newlines, so we demand at least 1 byte for decode
		// NOTICE: Ns*CNMiner.exe will send malformed JSON on very low diff, not sure we should handle this
		if len(data) > 1 {
			var req JSONRpcReq
			err = json.Unmarshal(data, &req)
			if err != nil {
				log.Printf("[Stratum] Malformed request from %s: %v", cs.ip, err)
				break
			}
			s.setDeadline(cs.conn)
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
		err := fmt.Errorf("[Stratum] Server disconnect request")
		log.Println(err)
		return err
	} else if req.Params == nil {
		err := fmt.Errorf("[Stratum] Server RPC request params")
		log.Println(err)
		return err
	}

	// Handle RPC methods
	switch req.Method {

	case "login":
		var params LoginParams

		err := json.Unmarshal(*req.Params, &params)
		if err != nil {
			log.Println("[Stratum] Unable to parse params")
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
		if err != nil {
			log.Println("[Stratum] Unable to parse params")
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
		if err != nil {
			log.Println("[Stratum] Unable to parse params")
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
		return fmt.Errorf("[Stratum] Server disconnect request")
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

func (s *StratumServer) registerMiner(miner *Miner) {
	s.miners.Set(miner.Id, miner)
}

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
			log.Printf("[Stratum] Upstream %v didn't pass check: %v", v.Name, err)
		}
		if ok && !backup {
			candidate = int32(i)
			backup = true
		}
	}

	if s.upstream != candidate {
		log.Printf("[Stratum] Switching to %v upstream", s.upstreams[candidate].Name)
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

// SetupCloseHandler creates a 'listener' on a new goroutine which will notify the
// program if it receives an interrupt from the OS. We then handle this by calling
// our clean up procedure and exiting the program.
// Reference: https://golangcode.com/handle-ctrl-c-exit-in-terminal/
func (s *StratumServer) SetupCloseHandler() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("\r- Ctrl+C pressed in Terminal")
		log.Printf("Closing - syncing miner stats...")
		err := Graviton_backend.WriteMinerStats(s.miners, s.hashrateExpiration)
		if err != nil {
			log.Printf("[Stratum] Err storing miner stats: %v", err)
		}
		Graviton_backend.DB.Close()
		os.Exit(0)
	}()
}
