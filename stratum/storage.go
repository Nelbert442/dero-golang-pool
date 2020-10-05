package stratum

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
	"git.dero.io/Nelbert442/dero-golang-pool/util"
	"github.com/deroproject/graviton"
)

type MiningShare struct {
	MinerID             string
	MinerJobID          string
	MinerJobNonce       string
	MinerJobResult      string
	SessionDiff         int64
	BlockTemplateHeight int64
	HashrateExpiration  time.Duration
	MinerIsSolo         bool
	MinerAddress        string
}

type MinerStats struct {
	Address      string
	Balance      int64
	LastShare    int64
	BlocksFound  int64
	TotalPending int64
	TotalPaid    int64
	MiningShares []*MiningShare
}

type GravitonMiners struct {
	Miners []*Miner
}

/*
type MinedBlocks struct {
	MinerID             string
	MinerJobID          string
	MinerJobNonce       string
	MinerJobResult      string
	SessionDiff         int64
	TemplateDiff        int64
	BlockTemplateHeight int64
	HashrateExpiration  time.Duration
	FeeReward           int64
	SubmitReplyBLID     string
	MinerIsSolo         bool
	MinerAddress        string
	BlockState          string
}
*/

type BlockDataGrav struct {
	Height         int64
	Timestamp      int64
	Difficulty     int64
	TotalShares    int64
	Orphan         bool
	Solo           bool
	Hash           string
	Address        string
	Nonce          string
	PowHash        string
	Reward         uint64
	ExtraReward    *big.Int
	ImmatureReward string
	RewardString   string
	RoundHeight    int64
	candidateKey   string
	immatureKey    string
	BlockState     string
}

type BlocksFoundByHeight struct {
	Heights map[int64]bool
}

type BlocksFound struct {
	MinedBlocks []*BlockDataGrav
}

type MinerPayments struct {
	Login     string
	TxHash    string
	TxKey     string
	TxFee     uint64
	Mixin     uint64
	Amount    uint64
	Timestamp int64
}

type ProcessedPayments struct {
	MinerPayments []*MinerPayments
}

type PaymentPending struct {
	Timestamp int64
	Amount    uint64
	Address   string
}

type PendingPayments struct {
	PendingPayout []*PaymentPending
}

type LastBlock struct {
	Difficulty string
	Height     int64
	Timestamp  int64
	Reward     int64
	Hash       string
}

type GravitonStore struct {
	DB     *graviton.Store
	DBPath string
	DBTree string
}

var Graviton_backend *GravitonStore = &GravitonStore{}

func (g *GravitonStore) NewGravDB(poolhost string) {
	current_path, err := os.Getwd()
	if err != nil {
		log.Printf("%v", err)
	}

	g.DBPath = filepath.Join(current_path, "pooldb")

	g.DB, _ = graviton.NewDiskStore(g.DBPath)

	g.DBTree = poolhost

	log.Printf("[Graviton] Initializing graviton store at path: %v", current_path)
}

func (g *GravitonStore) WriteBlocks(info *BlockDataGrav, blockType string) error {
	log.Printf("[Graviton] Inputting info: %v", info)
	confBytes, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal minedblocks info: %v", err)
	}

	// Store blocks found heights - only on candidate / found blocks by miners
	if blockType == "candidate" {
		err = g.WriteBlocksFoundByHeightArr(info.Height, info.Solo)
		if err != nil {
			log.Printf("[Graviton] Error writing blocksfoundbyheightarr: %v", err)
		}
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "block:" + blockType + ":" + strconv.FormatInt(info.Height, 10)
	tree.Put([]byte(key), []byte(confBytes)) // insert a value

	// Remove blocks from previous rounds (orphaned removes candidate, immature removes candidate, matured removes immature)
	switch blockType {
	case "orphaned":
		key := "block:" + "candidate" + ":" + strconv.FormatInt(info.Height, 10)

		log.Printf("[Graviton] Removing info: %v", key)
		err := tree.Delete([]byte(key))
		if err != nil {
			return err
		}
	case "immature":
		key := "block:" + "candidate" + ":" + strconv.FormatInt(info.Height, 10)

		log.Printf("[Graviton] Removing info: %v", key)
		err := tree.Delete([]byte(key))
		if err != nil {
			return err
		}
	case "matured":
		key := "block:" + "immature" + ":" + strconv.FormatInt(info.Height, 10)

		log.Printf("[Graviton] Removing info: %v", key)
		err := tree.Delete([]byte(key))
		if err != nil {
			return err
		}
	}

	graviton.Commit(tree) // commit the tree

	return nil
}

// Array of int64 [heights] of blocks found by pool, this does not include solo blocks found. Used as reference points for round hash calculations
func (g *GravitonStore) WriteBlocksFoundByHeightArr(height int64, isSolo bool) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "block:blocksFoundByHeight"
	currFoundByHeight, err := tree.Get([]byte(key))
	var foundByHeight *BlocksFoundByHeight

	var newFoundByHeight []byte

	if err != nil {
		heightArr := make(map[int64]bool)
		heightArr[height] = isSolo
		foundByHeight = &BlocksFoundByHeight{Heights: heightArr}
	} else {
		// Retrieve value and convert to BlocksFoundByHeight, so that you can manipulate and update db
		_ = json.Unmarshal(currFoundByHeight, &foundByHeight)

		foundByHeight.Heights[height] = isSolo
	}
	newFoundByHeight, err = json.Marshal(foundByHeight)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal foundByHeight info: %v", err)
	}

	tree.Put([]byte(key), newFoundByHeight)
	graviton.Commit(tree)

	return nil
}

func (g *GravitonStore) GetBlocksFoundByHeightArr() *BlocksFoundByHeight {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	currFoundByHeight, err := tree.Get([]byte("block:blocksFoundByHeight"))
	var foundByHeight *BlocksFoundByHeight

	if err != nil {
		return nil
	}

	_ = json.Unmarshal(currFoundByHeight, &foundByHeight)
	return foundByHeight
}

// Allow for getting the blocks found by pool/solo. blocktype: orphaned, candidate, immature, matured or specify all for returning all blocks
func (g *GravitonStore) GetBlocksFound(blocktype string) *BlocksFound {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config

	var foundByHeight *BlocksFoundByHeight
	var blocksFound *BlocksFound
	blocksFound = &BlocksFound{}
	blocksFoundByHeight, err := tree.Get([]byte("block:blocksFoundByHeight"))

	if err != nil {
		return nil
	}
	_ = json.Unmarshal(blocksFoundByHeight, &foundByHeight)
	for height := range foundByHeight.Heights {
		currHeight := int64(height)

		// Cycle through orphaned, candidates, immature, matured
		// Orphaned
		if blocktype == "orphaned" || blocktype == "all" {
			key := "block:orphaned:" + strconv.FormatInt(currHeight, 10)
			v, _ := tree.Get([]byte(key))
			if v != nil {
				var reply *BlockDataGrav
				_ = json.Unmarshal(v, &reply)
				blocksFound.MinedBlocks = append(blocksFound.MinedBlocks, reply)
			}
		}

		// Candidates
		if blocktype == "candidate" || blocktype == "all" {
			key := "block:candidate:" + strconv.FormatInt(currHeight, 10)
			v, _ := tree.Get([]byte(key))
			if v != nil {
				var reply *BlockDataGrav
				_ = json.Unmarshal(v, &reply)
				blocksFound.MinedBlocks = append(blocksFound.MinedBlocks, reply)
			}
		}

		// Immature
		if blocktype == "immature" || blocktype == "all" {
			key := "block:immature:" + strconv.FormatInt(currHeight, 10)
			v, _ := tree.Get([]byte(key))
			if v != nil {
				var reply *BlockDataGrav
				_ = json.Unmarshal(v, &reply)
				blocksFound.MinedBlocks = append(blocksFound.MinedBlocks, reply)
			}
		}

		// Matured
		if blocktype == "matured" || blocktype == "all" {
			key := "block:matured:" + strconv.FormatInt(currHeight, 10)
			v, _ := tree.Get([]byte(key))
			if v != nil {
				var reply *BlockDataGrav
				_ = json.Unmarshal(v, &reply)
				blocksFound.MinedBlocks = append(blocksFound.MinedBlocks, reply)
			}
		}
	}

	return blocksFound
}

func (g *GravitonStore) WriteImmatureBlock(block *BlockDataGrav) error {
	// Add to immature store
	immatureBlock := block
	immatureBlock.BlockState = "immature"

	// If block is not solo, set totalShares.
	// TODO: Configure share tracking for solo as well.
	if !block.Solo {
		_, totalShares, _ := g.GetRoundShares(block.Height)
		immatureBlock.TotalShares = totalShares
	}

	err := g.WriteBlocks(immatureBlock, "immature")
	if err != nil {
		log.Printf("[Graviton] Error when adding immature block store at height %v: %v", immatureBlock.Height, err)
	}

	return nil
}

func (g *GravitonStore) WriteMaturedBlocks(block *BlockDataGrav) error {
	// Add to matured store
	maturedBlock := block
	maturedBlock.BlockState = "matured"

	// If block is not solo, set totalShares.
	// TODO: Configure share tracking for solo as well.
	if !block.Solo {
		_, totalShares, _ := g.GetRoundShares(block.Height)
		maturedBlock.TotalShares = totalShares
	}

	err := g.WriteBlocks(maturedBlock, "matured")
	if err != nil {
		log.Printf("[Graviton] Error when adding matured block store at height %v: %v", maturedBlock.Height, err)
	}

	return nil
}

func (g *GravitonStore) WriteOrphanedBlocks(orphanedBlocks []*BlockDataGrav) error {
	// Remove blocks from candidates store and add them to orphaned block store
	for _, value := range orphanedBlocks {

		// Add to orphan store
		err := g.WriteBlocks(value, "orphaned")
		if err != nil {
			log.Printf("[Graviton] Error when adding orphaned block store at height %v: %v", value.Height, err)
			break
		}
	}

	return nil
}

// Function that will remove a k/v pair
func (g *GravitonStore) RemoveKey(key string) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config

	log.Printf("[Graviton] Removing info: %v", key)
	err := tree.Delete([]byte(key))
	if err != nil {
		return err
	}
	graviton.Commit(tree)

	return nil
}

func (g *GravitonStore) WriteImmaturePayments(info *PaymentPending) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "payments:immature"

	currPaymentsPending, err := tree.Get([]byte(key))
	var paymentsPending *PendingPayments

	var newPaymentsPending []byte

	if err != nil {
		// Returns key not found if != nil, or other err, but assuming keynotfound/leafnotfound
		var paymentsPendingArr []*PaymentPending
		paymentsPendingArr = append(paymentsPendingArr, info)
		paymentsPending = &PendingPayments{PendingPayout: paymentsPendingArr}
	} else {
		// Retrieve value and convert to BlocksFoundByHeight, so that you can manipulate and update db
		_ = json.Unmarshal(currPaymentsPending, &paymentsPending)

		paymentsPending.PendingPayout = append(paymentsPending.PendingPayout, info)
	}
	newPaymentsPending, err = json.Marshal(paymentsPending)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal paymentsPending info: %v", err)
	}

	tree.Put([]byte(key), newPaymentsPending)
	graviton.Commit(tree)

	v, err := tree.Get([]byte(key))
	if v != nil {
		var reply *PendingPayments
		_ = json.Unmarshal(v, &reply)
	}

	return nil
}

func (g *GravitonStore) WritePendingPayments(info *PaymentPending) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "payments:pending"

	currPaymentsPending, err := tree.Get([]byte(key))
	var paymentsPending *PendingPayments

	var newPaymentsPending []byte

	if err != nil {
		// Returns key not found if != nil, or other err, but assuming keynotfound/leafnotfound
		var paymentsPendingArr []*PaymentPending
		paymentsPendingArr = append(paymentsPendingArr, info)
		paymentsPending = &PendingPayments{PendingPayout: paymentsPendingArr}
	} else {
		// Retrieve value and convert to BlocksFoundByHeight, so that you can manipulate and update db
		_ = json.Unmarshal(currPaymentsPending, &paymentsPending)

		// Check through existing pending payments and append amount if login already has a pending amount
		var updateExisting bool
		for p, currPayment := range paymentsPending.PendingPayout {
			if info.Address == currPayment.Address {
				log.Printf("[Graviton] Updating value for %v from %v to %v", info.Address, paymentsPending.PendingPayout[p].Amount, (paymentsPending.PendingPayout[p].Amount + info.Amount))
				paymentsPending.PendingPayout[p].Amount += info.Amount
				updateExisting = true
			}
		}

		// If an existing payment was not upated since the addresses didn't match, append the new payment
		if !updateExisting {
			log.Printf("[Graviton] Appending new payment: %v", info)
			paymentsPending.PendingPayout = append(paymentsPending.PendingPayout, info)
		}
	}
	newPaymentsPending, err = json.Marshal(paymentsPending)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal paymentsPending info: %v", err)
	}

	tree.Put([]byte(key), newPaymentsPending)
	graviton.Commit(tree)

	v, err := tree.Get([]byte(key))
	if v != nil {
		var reply *PendingPayments
		_ = json.Unmarshal(v, &reply)
	}

	return nil
}

func (g *GravitonStore) GetPendingPayments() []*PaymentPending {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "payments:pending"
	var reply *PendingPayments

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)
		return reply.PendingPayout
	}

	return nil
}

// This function is to overwrite pending payments in the event of 'deleting' a pending payment after payment has been processed
func (g *GravitonStore) OverwritePendingPayments(info *PendingPayments) error {
	confBytes, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal pendingpayments info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "payments:pending"

	tree.Put([]byte(key), confBytes)
	graviton.Commit(tree)

	return nil
}

func (g *GravitonStore) WriteProcessedPayments(info *MinerPayments) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "payments:processed"

	currPaymentsProcessed, err := tree.Get([]byte(key))
	var paymentsProcessed *ProcessedPayments

	var newPaymentsProcessed []byte

	if err != nil {
		// Returns key not found if != nil, or other err, but assuming keynotfound/leafnotfound
		var paymentsProcessedArr []*MinerPayments
		paymentsProcessedArr = append(paymentsProcessedArr, info)
		paymentsProcessed = &ProcessedPayments{MinerPayments: paymentsProcessedArr}
	} else {
		// Retrieve value and convert to BlocksFoundByHeight, so that you can manipulate and update db
		_ = json.Unmarshal(currPaymentsProcessed, &paymentsProcessed)

		paymentsProcessed.MinerPayments = append(paymentsProcessed.MinerPayments, info)
	}
	newPaymentsProcessed, err = json.Marshal(paymentsProcessed)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal paymentsProcessed info: %v", err)
	}

	tree.Put([]byte(key), newPaymentsProcessed)
	graviton.Commit(tree)

	return nil
}

func (g *GravitonStore) GetProcessedPayments() *ProcessedPayments {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "payments:processed"
	var reply *ProcessedPayments

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)
		return reply
	}

	return nil
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

func (blockDataGrav *BlockDataGrav) RoundKey() string {
	return join(blockDataGrav.RoundHeight, blockDataGrav.Hash)
}

func (g *GravitonStore) WriteLastBlock(lastBlock *LastBlock) error {
	confBytes, err := json.Marshal(lastBlock)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal lastblock info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "lastblock"
	tree.Put([]byte(key), []byte(confBytes)) // insert a value
	graviton.Commit(tree)                    // commit the tree

	return nil
}

func (g *GravitonStore) GetLastBlock() *LastBlock {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "lastblock"
	var reply *LastBlock

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)
		return reply
	}

	return nil
}

func (g *GravitonStore) WriteConfig(config *pool.Config) error {
	confBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal pool.Config info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "config:" + config.Coin
	tree.Put([]byte(key), []byte(confBytes)) // insert a value
	graviton.Commit(tree)                    // commit the tree

	return nil
}

func (g *GravitonStore) GetConfig(coin string) *pool.Config {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "config:" + coin
	var reply *pool.Config

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)
		return reply
	}

	return nil
}

func (g *GravitonStore) WriteMinerIDRegistration(miner *Miner) error {
	log.Printf("[Graviton] Registering miner: %v", miner.Id)
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "miners:registered"
	currMinerIDs, err := tree.Get([]byte(key))
	var minerIDs *GravitonMiners

	var newMinerIDs []byte

	if err != nil {
		// Returns key not found if != nil, or other err, but assuming keynotfound/leafnotfound
		var minerIDArr []*Miner
		minerIDArr = append(minerIDArr, miner)
		minerIDs = &GravitonMiners{Miners: minerIDArr}
	} else {
		// Retrieve value and convert to minerids, so that you can manipulate and update db
		_ = json.Unmarshal(currMinerIDs, &minerIDs)

		for _, value := range minerIDs.Miners {
			if value.Id == miner.Id {
				log.Printf("[Graviton] Miner already registered: %v", miner.Id)
				return nil
			}
		}

		minerIDs.Miners = append(minerIDs.Miners, miner)
	}

	// Since we know the miner is not already registered [would have returned out above if it were], we can store into miner stats to prep stats for later
	confMiner, err := json.Marshal(miner)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal miner info: %v", err)
	}
	mk := "miners:stats:" + miner.Id
	tree.Put([]byte(mk), confMiner)

	newMinerIDs, err = json.Marshal(minerIDs)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal minerIDs info: %v", err)
	}

	tree.Put([]byte(key), newMinerIDs)
	graviton.Commit(tree)

	return nil
}

func (g *GravitonStore) GetMinerIDRegistrations() []*Miner {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "miners:registered"
	var reply *GravitonMiners

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)
		return reply.Miners
	}

	return nil
}

func (g *GravitonStore) CompareMinerStats(storedMiner, miner *Miner, hashrateExpiration time.Duration) *Miner {
	// Get existing, compare the roundShares of each...
	// Check online etc.
	// Compare storedMiner to miner input, update the stored miner with a few 'appends'
	blockHeightArr := Graviton_backend.GetBlocksFoundByHeightArr()
	updatedMiner := miner
	var oldHashes bool
	var oldHashesHeight int64

	if storedMiner != nil {
		if blockHeightArr != nil {
			for height, solo := range blockHeightArr.Heights {
				if storedMiner.RoundHeight <= height && !solo {
					// Miner round height is less than a pre-found block [usually happens for disconnected miners]. Reset counters
					oldHashes = true
					oldHashesHeight = height
				}
			}
		}

		if !oldHashes && updatedMiner != nil {
			// In the event that the stored shares is greater than mem shares, stored shares = mem shares + stored shares (difference of)
			if storedMiner.RoundShares >= updatedMiner.RoundShares { //|| storedMiner.RoundHeight <= updatedMiner.RoundHeight {
				diff := storedMiner.RoundShares - updatedMiner.RoundShares
				if diff < 0 {
					diff = 0
				}
				updatedMiner.RoundShares += diff

				// Remove old shares from backend - older than hashrate expiration of pool config
				now := util.MakeTimestamp() / 1000
				hashExpiration := int64(hashrateExpiration / time.Second)

				for k, _ := range updatedMiner.Shares {
					if k < now-hashExpiration {
						delete(updatedMiner.Shares, k)
					}
				}
			}
		} else if oldHashes && updatedMiner == nil {
			// If no current miner, but new round is defined, set roundShares to 0 since their stored shares are not counted anymore
			updatedMiner := storedMiner
			updatedMiner.Lock()
			if updatedMiner.RoundShares != 0 {
				updatedMiner.RoundHeight = oldHashesHeight
				updatedMiner.RoundShares = 0
			}

			// Remove old shares from backend - older than hashrate expiration of pool config
			now := util.MakeTimestamp() / 1000
			hashExpiration := int64(hashrateExpiration / time.Second)

			for k, _ := range updatedMiner.Shares {
				if k < now-hashExpiration {
					delete(updatedMiner.Shares, k)
				}
			}

			updatedMiner.Unlock()
		}
	}

	if updatedMiner != nil {
		return updatedMiner
	} else {
		return storedMiner
	}
}

func (g *GravitonStore) WriteMinerStats(miners MinersMap, hashrateExpiration time.Duration) error {
	var confBytes []byte
	var err error
	storedMinerSlice := g.GetAllMinerStats()

	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config

	// If storedMinerMap is empty, set it to miners
	if storedMinerSlice != nil {
		for _, storedMiner := range storedMinerSlice {
			currMiner, _ := miners.Get(storedMiner.Id)
			updatedMiner := g.CompareMinerStats(storedMiner, currMiner, hashrateExpiration)

			confBytes, err = json.Marshal(updatedMiner)
			if err != nil {
				return fmt.Errorf("[Graviton] could not marshal miner stats: %v", err)
			}

			key := "miners:stats:" + updatedMiner.Id // TODO: Append on the miner ID
			tree.Put([]byte(key), []byte(confBytes)) // insert a value
		}
	} else {
		registeredMiners := g.GetMinerIDRegistrations()

		for _, value := range registeredMiners {
			currMiner, _ := miners.Get(value.Id)

			if currMiner != nil {
				confBytes, err = json.Marshal(currMiner)
				if err != nil {
					return fmt.Errorf("[Graviton] could not marshal miner stats: %v", err)
				}

				key := "miners:stats:" + currMiner.Id    // TODO: Append on the miner ID
				tree.Put([]byte(key), []byte(confBytes)) // insert a value
			}
		}
	}

	graviton.Commit(tree) // commit the tree

	return nil
}

func (g *GravitonStore) WriteMinerStatsByID(miner *Miner, hashrateExpiration time.Duration) error {
	storedMiner := g.GetMinerStatsByID(miner.Id)

	updatedMiner := g.CompareMinerStats(storedMiner, miner, hashrateExpiration)

	confBytes, err := json.Marshal(updatedMiner)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal pool.Config info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "miners:stats:" + updatedMiner.Id
	tree.Put([]byte(key), []byte(confBytes)) // insert a value
	graviton.Commit(tree)                    // commit the tree

	return nil
}

func (g *GravitonStore) GetAllMinerStats() []*Miner {
	var allMiners []*Miner
	registeredMiners := g.GetMinerIDRegistrations()

	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config

	for _, value := range registeredMiners {
		key := "miners:stats:" + value.Id
		var reply *Miner

		v, _ := tree.Get([]byte(key))

		if v != nil {
			_ = json.Unmarshal(v, &reply)
			allMiners = append(allMiners, reply)
		}
	}

	if allMiners != nil {
		return allMiners
	}

	return nil
}

func (g *GravitonStore) GetMinerStatsByID(minerID string) *Miner {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "miners:stats:" + minerID

	var reply *Miner

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)
		return reply
	}

	return nil
}

func (g *GravitonStore) GetRoundShares(roundHeight int64) (map[string]int64, int64, error) {

	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "miners:round:" + strconv.FormatInt(roundHeight, 10)

	var result map[string]int64
	var totalRoundShares int64

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &result)
	}

	for _, value := range result {
		totalRoundShares += value
	}

	return result, totalRoundShares, nil
}

func (g *GravitonStore) WriteRoundShares(roundHeight int64, roundShares map[string]int64) error {
	confBytes, err := json.Marshal(roundShares)
	if err != nil {
		return fmt.Errorf("[Graviton] could not marshal roundShares info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "miners:round:" + strconv.FormatInt(roundHeight, 10)
	log.Printf("[Graviton-WriteRoundShares] Storing %v with values: %v", key, roundShares)
	tree.Put([]byte(key), []byte(confBytes)) // insert a value
	graviton.Commit(tree)                    // commit the tree

	return nil
}

func (g *GravitonStore) NextRound(roundHeight int64, hashrateExpiration time.Duration) error {
	//registeredMiners := g.GetMinerIDRegistrations()
	miners := g.GetAllMinerStats()
	roundShares := make(map[string]int64)

	for _, currMiner := range miners {
		//currMiner, _ := miners.Get(value.Id)
		if currMiner != nil {
			// If the current miner roundheight is equal to roundheight, then add roundshares and lastroundshares to roundShares map
			if currMiner.RoundHeight == roundHeight {
				roundShares[currMiner.Address] += currMiner.RoundShares
				roundShares[currMiner.Address] += currMiner.LastRoundShares
			}

			if currMiner.RoundShares != 0 || currMiner.LastRoundShares != 0 {
				currMiner.RoundShares = 0
				currMiner.LastRoundShares = 0
				err := g.WriteMinerStatsByID(currMiner, hashrateExpiration)

				if err != nil {
					log.Printf("[Graviton-NextRound] Error when writing miner stats (%v) to DB for next round: %v", currMiner.Id, err)
				}
			}
		}
	}

	// Writes the round shares for all of the miners
	g.WriteRoundShares(roundHeight, roundShares)

	return nil
}
