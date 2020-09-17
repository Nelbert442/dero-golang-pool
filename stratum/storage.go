package stratum

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"time"

	"git.dero.io/Nelbert442/dero-golang-pool/pool"
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
	//Heights []int64
	Heights map[int64]bool
}

type BlocksFound struct {
	MinedBlocks []*BlockDataGrav
}

type MinerPayments struct {
	Login  string
	TxHash string
	TxKey  string
	TxFee  uint64
	Mixin  uint64
	Amount uint64
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

	g.DBPath = current_path + "\\pooldb"

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
	graviton.Commit(tree)                    // commit the tree

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
		//log.Printf("[Graviton] Error on get: %v. err: %v", key, err)
		// Returns key not found if != nil, or other err, but assuming keynotfound/leafnotfound
		//var heightArr []int64
		//heightArr = append(heightArr, height)
		heightArr := make(map[int64]bool)
		heightArr[height] = isSolo
		foundByHeight = &BlocksFoundByHeight{Heights: heightArr}
	} else {
		// Retrieve value and convert to BlocksFoundByHeight, so that you can manipulate and update db
		_ = json.Unmarshal(currFoundByHeight, &foundByHeight)

		//foundByHeight.Heights = append(foundByHeight.Heights, height)
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
		//log.Printf("No block details within block:blocksFoundByHeight")
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

	//log.Printf("[Graviton] Retrieving stored minedblocks...")

	var foundByHeight *BlocksFoundByHeight
	var blocksFound *BlocksFound
	blocksFound = &BlocksFound{}
	blocksFoundByHeight, err := tree.Get([]byte("block:blocksFoundByHeight"))

	if err != nil {
		//log.Printf("Err getting blocksFoundByHeight: %v", err)
		return nil
	}
	_ = json.Unmarshal(blocksFoundByHeight, &foundByHeight)
	for height := range foundByHeight.Heights {
		currHeight := int64(height)

		// Cycle through orphaned, candidates, immature, matured
		// Orphaned
		if blocktype == "orphaned" || blocktype == "all" {
			key := "block:orphaned:" + strconv.FormatInt(currHeight, 10)
			//log.Printf("Retrieving stored orphaned blocks: %v...", key)
			v, _ := tree.Get([]byte(key))
			if v != nil {
				var reply *BlockDataGrav
				_ = json.Unmarshal(v, &reply)
				blocksFound.MinedBlocks = append(blocksFound.MinedBlocks, reply)
				//log.Printf("[Graviton] key=%s, value=%v\n", key, reply)
			}
		}

		// Candidates
		if blocktype == "candidate" || blocktype == "all" {
			key := "block:candidate:" + strconv.FormatInt(currHeight, 10)
			//log.Printf("Retrieving stored candidate blocks: %v...", key)
			v, _ := tree.Get([]byte(key))
			if v != nil {
				var reply *BlockDataGrav
				_ = json.Unmarshal(v, &reply)
				blocksFound.MinedBlocks = append(blocksFound.MinedBlocks, reply)
				//log.Printf("[Graviton] key=%s, value=%v\n", key, reply)
			}
		}

		// Immature
		if blocktype == "immature" || blocktype == "all" {
			key := "block:immature:" + strconv.FormatInt(currHeight, 10)
			//log.Printf("Retrieving stored immature blocks: %v...", key)
			v, _ := tree.Get([]byte(key))
			if v != nil {
				var reply *BlockDataGrav
				_ = json.Unmarshal(v, &reply)
				blocksFound.MinedBlocks = append(blocksFound.MinedBlocks, reply)
				//log.Printf("[Graviton] key=%s, value=%v\n", key, reply)
			}
		}

		// Matured
		if blocktype == "matured" || blocktype == "all" {
			key := "block:matured:" + strconv.FormatInt(currHeight, 10)
			//log.Printf("Retrieving stored matured blocks: %v...", key)
			v, _ := tree.Get([]byte(key))
			if v != nil {
				var reply *BlockDataGrav
				_ = json.Unmarshal(v, &reply)
				blocksFound.MinedBlocks = append(blocksFound.MinedBlocks, reply)
				//log.Printf("[Graviton] key=%s, value=%v\n", key, reply)
			}
		}
	}

	return blocksFound
}

func (g *GravitonStore) WriteImmatureBlock(block *BlockDataGrav) error {
	// Add to immature store
	immatureBlock := block
	immatureBlock.BlockState = "immature"
	//log.Printf("[Graviton] Adding block to immature store: %v", immatureBlock)
	err := g.WriteBlocks(immatureBlock, "immature")
	if err != nil {
		log.Printf("[Graviton] Error when adding immature block store at height %v: %v", immatureBlock.Height, err)
	}

	// Remove candidate store after no err from adding to orphan store
	//log.Printf("[Graviton] Removing block from candidate store: %v", block)
	err = g.RemoveBlock("candidate", block.Height)
	if err != nil {
		log.Printf("[Graviton] Error when removing candidate block store at height %v: %v", block.Height, err)
	}

	return nil
}

func (g *GravitonStore) WriteMaturedBlocks(block *BlockDataGrav) error {
	// Add to matured store
	maturedBlock := block
	maturedBlock.BlockState = "matured"
	//log.Printf("[Graviton] Adding block to matured store: %v", maturedBlock)
	err := g.WriteBlocks(maturedBlock, "matured")
	if err != nil {
		log.Printf("[Graviton] Error when adding matured block store at height %v: %v", maturedBlock.Height, err)
	}

	// Remove immature store after no err from adding to orphan store
	//log.Printf("[Graviton] Removing block from immature store: %v", block)
	err = g.RemoveBlock("immature", block.Height)
	if err != nil {
		log.Printf("[Graviton] Error when removing immature block store at height %v: %v", block.Height, err)
	}

	return nil
}

func (g *GravitonStore) WriteOrphanedBlocks(orphanedBlocks []*BlockDataGrav) error {
	// Remove blocks from candidates store and add them to orphaned block store
	for _, value := range orphanedBlocks {

		// Add to orphan store
		//log.Printf("[Graviton] Adding block to orphaned store: %v", value)
		err := g.WriteBlocks(value, "orphaned")
		if err != nil {
			log.Printf("[Graviton] Error when adding orphaned block store at height %v: %v", value.Height, err)
			break
		}

		// Remove candidate store after no err from adding to orphan store
		//log.Printf("[Graviton] Removing block from candidate store: %v", value)
		err = g.RemoveBlock("candidate", value.Height)
		if err != nil {
			log.Printf("[Graviton] Error when removing candidate block store at height %v: %v", value.Height, err)
			break
		}
	}

	return nil
}

// Function that will remove a k/v pair, generally only used to remove candidate/immature block stores as they move from candidate --> immature / orphaned --> matured
func (g *GravitonStore) RemoveBlock(blockType string, blockheight int64) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config

	key := "block:" + blockType + ":" + strconv.FormatInt(blockheight, 10)

	//log.Printf("[Graviton] Removing k/v pair: %v", key)
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
		//log.Printf("[Graviton] Error on get: %v. err: %v", key, err)
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
		//log.Printf("[Graviton] key=%v, value=%v\n", key, reply.PendingPayout)
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
		//log.Printf("[Graviton] Error on get: %v. err: %v", key, err)
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
		//log.Printf("[Graviton] key=%v, value=%v\n", key, reply.PendingPayout)
	}

	return nil
}

func (g *GravitonStore) GetPendingPayments() []*PaymentPending {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "payments:pending"
	var reply *PendingPayments

	//log.Printf("[Graviton] Retrieving stored %v...", key)

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)
		return reply.PendingPayout
	}

	return nil
}

// This function is to overwrite pending payments in the event of 'deleting' a pending payment after payment has been processed
func (g *GravitonStore) OverwritePendingPayments(info *PendingPayments) error {
	//log.Printf("[Graviton] Pruning payments to match: %v", info)
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
		//log.Printf("[Graviton] Error on get: %v. err: %v", key, err)
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

	v, err := tree.Get([]byte(key))
	if v != nil {
		var reply *ProcessedPayments
		_ = json.Unmarshal(v, &reply)
		//log.Printf("[Graviton] key=%v, value=%v\n", key, reply.MinerPayments)
	}

	return nil
}

func (g *GravitonStore) GetProcessedPayments() *ProcessedPayments {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "payments:processed"
	var reply *ProcessedPayments

	//log.Printf("[Graviton] Retrieving stored %v...", key)

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)
		return reply
	}

	return nil
}

func (blockDataGrav *BlockDataGrav) RoundKey() string {
	return join(blockDataGrav.RoundHeight, blockDataGrav.Hash)
}

func (g *GravitonStore) WriteLastBlock(lastBlock *LastBlock) error {
	//log.Printf("Inputting info: %v", lastBlock)
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

	//log.Printf("[Graviton] Retrieving stored lastBlock...")

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)
		return reply
	}

	return nil
}

func (g *GravitonStore) WriteConfig(config *pool.Config) error {
	//log.Printf("[Graviton] Inputting info: %v", config)
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

	//log.Printf("[Graviton] Retrieving stored config...")

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
		//log.Printf("[Graviton] Error on get: miners:registered. err: %v", err)
		// Returns key not found if != nil, or other err, but assuming keynotfound/leafnotfound
		var minerIDArr []*Miner
		minerIDArr = append(minerIDArr, miner)
		minerIDs = &GravitonMiners{Miners: minerIDArr}
	} else {
		// Retrieve value and convert to minerids, so that you can manipulate and update db
		_ = json.Unmarshal(currMinerIDs, &minerIDs)

		for _, value := range minerIDs.Miners {
			//log.Printf("Comparing for registration: %v && %v", value, miner.Id)
			if value.Id == miner.Id {
				log.Printf("[Graviton] Miner already registered: %v", miner.Id)
				return nil
			}
		}

		minerIDs.Miners = append(minerIDs.Miners, miner)
	}

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

	//log.Printf("[Graviton] Retrieving stored %v...", key)

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)
		return reply.Miners
	}

	return nil
}

func (g *GravitonStore) CompareMinerStats(storedMinerMap *MinersMap, storedMiner, miner *Miner) *MinersMap {
	// Get existing, compare the roundShares of each...
	// Check online etc.
	// Compare storedMiner to miner input, update the stored miner with a few 'appends'
	blockHeightArr := Graviton_backend.GetBlocksFoundByHeightArr()
	updatedMiner := miner
	var oldHashes bool

	if storedMiner != nil {
		if blockHeightArr != nil {
			for height, solo := range blockHeightArr.Heights {
				if storedMiner.RoundHeight <= height && !solo {
					// Miner round height is less than a pre-found block [usually happens for disconnected miners]. Reset counters
					oldHashes = true
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
			}
		} else if oldHashes && updatedMiner == nil {
			// If no current miner, but new round is defined, set roundShares to 0 since their stored shares are not counted anymore
			updatedMiner := storedMiner
			updatedMiner.RoundShares = 0
		}
	}

	// Set the updatedMiner within storedMinerMap
	// Validate updatedMiner exists, then store, otherwise return the store
	if updatedMiner != nil {
		storedMinerMap.Set(updatedMiner.Id, updatedMiner)
	}

	return storedMinerMap
}

func (g *GravitonStore) WriteMinerStats(miners MinersMap) error {
	var confBytes []byte
	var err error
	storedMinerMap := g.GetAllMinerStats()
	// If storedMinerMap is empty, set it to miners
	if storedMinerMap != nil {
		if !storedMinerMap.IsEmpty() {
			registeredMiners := g.GetMinerIDRegistrations()

			for _, value := range registeredMiners {
				storedMiner, _ := storedMinerMap.Get(value.Id)
				currMiner, _ := miners.Get(value.Id)

				/*
					if storedMiner != nil {
						log.Printf("Checking for stored roundshare: %v", storedMiner.RoundShares)
					}
					if currMiner != nil {
						log.Printf("Checking for curr miner roundshare: %v", currMiner.RoundShares)
					}
				*/

				// Make sure that both stored & curr miners exist prior to doing compareminerstats.
				//if currMiner != nil {
				storedMinerMap = g.CompareMinerStats(storedMinerMap, storedMiner, currMiner)
				//}
			}

			confBytes, err = json.Marshal(storedMinerMap)
			if err != nil {
				return fmt.Errorf("[Graviton] could not marshal miner stats: %v", err)
			}
		} else {
			confBytes, err = json.Marshal(miners)
			if err != nil {
				return fmt.Errorf("[Graviton] could not marshal miner stats: %v", err)
			}
		}
	} else {
		confBytes, err = json.Marshal(miners)
		if err != nil {
			return fmt.Errorf("[Graviton] could not marshal miner stats: %v", err)
		}
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "miners:stats"
	tree.Put([]byte(key), []byte(confBytes)) // insert a value
	graviton.Commit(tree)                    // commit the tree

	return nil
}

func (g *GravitonStore) GetAllMinerStats() *MinersMap {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "miners:stats"

	var reply *MinersMap

	//log.Printf("[Graviton] Retrieving stored miners map...")

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)
		return reply
	}

	return nil
}

func (g *GravitonStore) GetMinerStatsByID(minerID string) *Miner {
	store := g.DB
	ss, _ := store.LoadSnapshot(0)  // load most recent snapshot
	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "miners:stats"

	var reply MinersMap

	//log.Printf("[Graviton] Retrieving stored miners stats... %v", minerID)

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &reply)

		miner, _ := reply.Get(minerID)
		return miner
	}

	return nil
}

func (g *GravitonStore) GetRoundShares(roundHeight int64, nonce string) (map[string]int64, int64, error) {

	// Get all of the registered miners
	minerIDRegistrations := g.GetMinerIDRegistrations()
	result := make(map[string]int64)
	var totalRoundShares int64

	// Loop through registered miners looking for
	for _, value := range minerIDRegistrations {
		miner := g.GetMinerStatsByID(value.Id)
		result[miner.Address] = miner.LastRoundShares[roundHeight]
		totalRoundShares += miner.LastRoundShares[roundHeight]
	}

	return result, totalRoundShares, nil
}

func (g *GravitonStore) NextRound(roundHeight int64, miners MinersMap) error {
	registeredMiners := g.GetMinerIDRegistrations()

	for _, value := range registeredMiners {
		currMiner, _ := miners.Get(value.Id)
		if currMiner != nil {
			// Run storeShare function, will update roundshares and lastroundshares w/ height
			currMiner.storeShare(0, roundHeight)
			log.Printf("[Graviton] Setting %v roundShares to %v and lastroundshares[%v] to %v", currMiner.Id, currMiner.RoundShares, roundHeight, currMiner.LastRoundShares[roundHeight])
		}
	}

	return nil
}
