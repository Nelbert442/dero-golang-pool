package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
)

type RPCClient struct {
	sync.RWMutex
	sickRate         int64
	successRate      int64
	Accepts          int64
	Rejects          int64
	LastSubmissionAt int64
	FailsCount       int64
	Url              *url.URL
	login            string
	password         string
	Name             string
	sick             bool
	client           *http.Client
	info             atomic.Value
}

type GetBlockTemplateReply struct {
	Blocktemplate_blob string `json:"blocktemplate_blob"`
	Blockhashing_blob  string `json:"blockhashing_blob"`
	Expected_reward    uint64 `json:"expected_reward"`
	Difficulty         uint64 `json:"difficulty"`
	Height             uint64 `json:"height"`
	Prev_Hash          string `json:"prev_hash"`
	Reserved_Offset    uint64 `json:"reserved_offset"`
	Epoch              uint64 `json:"epoch"` // used to expire pool jobs
	Status             string `json:"status"`
}

type SubmitBlock_Result struct {
	BLID   string `json:"blid"`
	Status string `json:"status"`
}

type GetInfoReply struct {
	Difficulty         int64   `json:"difficulty"`
	Stableheight       int64   `json:"stableheight"`
	Topoheight         int64   `json:"topoheight"`
	Averageblocktime50 float32 `json:"averageblocktime50"`
	Target             int64   `json:"target"`
	Testnet            bool    `json:"testnet"`
	TopBlockHash       string  `json:"top_block_hash"`
	DynamicFeePerKB    int64   `json:"dynamic_fee_per_kb"`
	TotalSupply        int64   `json:"total_supply"`
	MedianBlockSize    int64   `json:"median_block_Size"`
	Version            string  `json:"version"`
	Height             int64   `json:"height"`
	TxPoolSize         int64   `json:"tx_pool_size"`
	Status             string  `json:"status"`
}

type GetBlockHashReply struct {
	BlockHeader Block_Header `json:"block_header"`
	Status      string       `json:"status"`
}

type GetBalanceReply struct {
	Balance         uint64 `json:"balance"`
	UnlockedBalance uint64 `json:"unlocked_balance"`
}

type Block_Header struct {
	Depth        int64    `json:"depth"`
	Difficulty   string   `json:"difficulty"`
	Hash         string   `json:"hash"`
	Height       int64    `json:"height"`
	Topoheight   int64    `json:"topoheight"`
	MajorVersion uint64   `json:"major_version"`
	MinorVersion uint64   `json:"minor_version"`
	Nonce        uint64   `json:"nonce"`
	OrphanStatus bool     `json:"orphan_status"`
	Syncblock    bool     `json:"syncblock"`
	Txcount      int64    `json:"txcount"`
	Reward       uint64   `json:"reward"`
	Tips         []string `json:"tips"`
	Timestamp    uint64   `json:"timestamp"`
}

type JSONRpcResp struct {
	Id     *json.RawMessage       `json:"id"`
	Result *json.RawMessage       `json:"result"`
	Error  map[string]interface{} `json:"error"`
}

func NewRPCClient(cfg *pool.Upstream) (*RPCClient, error) {
	rawUrl := fmt.Sprintf("http://%s:%v/json_rpc", cfg.Host, cfg.Port)
	url, err := url.Parse(rawUrl)
	if err != nil {
		return nil, err
	}
	rpcClient := &RPCClient{Name: cfg.Name, Url: url}
	timeout, _ := time.ParseDuration(cfg.Timeout)
	rpcClient.client = &http.Client{
		Timeout: timeout,
	}
	return rpcClient, nil
}

func (r *RPCClient) GetBlockTemplate(reserveSize int, address string) (*GetBlockTemplateReply, error) {
	params := map[string]interface{}{"reserve_size": reserveSize, "wallet_address": address}
	rpcResp, err := r.doPost(r.Url.String(), "getblocktemplate", params)
	var reply *GetBlockTemplateReply
	if err != nil {
		return nil, err
	}
	if rpcResp.Result != nil {
		err = json.Unmarshal(*rpcResp.Result, &reply)
	}
	return reply, err
}

func (r *RPCClient) GetBlockByHash(hash string) (*GetBlockHashReply, error) {
	params := map[string]interface{}{"hash": hash}
	rpcResp, err := r.doPost(r.Url.String(), "getblockheaderbyhash", params)
	if err != nil {
		return nil, err
	}

	var reply *GetBlockHashReply
	if rpcResp.Result != nil {
		err = json.Unmarshal(*rpcResp.Result, &reply)
	}

	return reply, err
}

func (r *RPCClient) GetInfo() (*GetInfoReply, error) {
	params := make(map[string]interface{})
	rpcResp, err := r.doPost(r.Url.String(), "get_info", params)
	var reply *GetInfoReply
	if err != nil {
		return nil, err
	}
	if rpcResp.Result != nil {
		err = json.Unmarshal(*rpcResp.Result, &reply)
	}
	return reply, err
}

func (r *RPCClient) GetBalance(url string) (*GetBalanceReply, error) {

	rpcResp, err := r.doPost(url, "getbalance", []string{})
	if err != nil {
		return nil, err
	}
	var reply *GetBalanceReply
	err = json.Unmarshal(*rpcResp.Result, &reply)
	if err != nil {
		return nil, err
	}
	return reply, err
}

func (r *RPCClient) SubmitBlock(blocktemplate_blob string, blockhashing_blob string) (*JSONRpcResp, error) {
	return r.doPost(r.Url.String(), "submitblock", []string{blocktemplate_blob, blockhashing_blob})
}

func (r *RPCClient) doPost(url, method string, params interface{}) (*JSONRpcResp, error) {
	jsonReq := map[string]interface{}{"jsonrpc": "2.0", "id": 0, "method": method, "params": params}
	data, _ := json.Marshal(jsonReq)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	req.Header.Set("Content-Length", (string)(len(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(r.login, r.password)
	resp, err := r.client.Do(req)
	if err != nil {
		r.markSick()
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, errors.New(resp.Status)
	}

	var rpcResp *JSONRpcResp
	err = json.NewDecoder(resp.Body).Decode(&rpcResp)
	if err != nil {
		r.markSick()
		return nil, err
	}
	if rpcResp.Error != nil {
		r.markSick()
		return nil, errors.New(rpcResp.Error["message"].(string))
	}
	return rpcResp, err
}

func (r *RPCClient) Check(reserveSize int, address string) (bool, error) {
	_, err := r.GetBlockTemplate(reserveSize, address)
	if err != nil {
		return false, err
	}
	r.markAlive()
	return !r.Sick(), nil
}

func (r *RPCClient) Sick() bool {
	r.RLock()
	defer r.RUnlock()
	return r.sick
}

func (r *RPCClient) markSick() {
	r.Lock()
	if !r.sick {
		atomic.AddInt64(&r.FailsCount, 1)
	}
	r.sickRate++
	r.successRate = 0
	if r.sickRate >= 5 {
		r.sick = true
	}
	r.Unlock()
}

func (r *RPCClient) markAlive() {
	r.Lock()
	r.successRate++
	if r.successRate >= 5 {
		r.sick = false
		r.sickRate = 0
		r.successRate = 0
	}
	r.Unlock()
}

func (r *RPCClient) UpdateInfo() (*GetInfoReply, error) {
	info, err := r.GetInfo()
	if err == nil {
		r.info.Store(info)
	}
	return info, err
}

func (r *RPCClient) Info() *GetInfoReply {
	reply, _ := r.info.Load().(*GetInfoReply)
	return reply
}
