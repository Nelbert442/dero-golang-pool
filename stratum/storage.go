package stratum

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Nelbert442/dero-golang-pool/pool"
	"github.com/deroproject/graviton"
)

type PoolRound struct {
	StartTimestamp  int64
	Timestamp       int64
	LastBlockHeight int64
	RoundShares     map[string]int64
}

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

type GravitonCharts struct {
	Values []*ChartData
}

type BlockDataGrav struct {
	Height      int64
	Timestamp   int64
	Difficulty  int64
	TotalShares int64
	Orphan      bool
	Solo        bool
	Hash        string
	Address     string
	Nonce       string
	PowHash     string
	Reward      uint64
	ExtraReward *big.Int
	RoundHeight int64
	BlockState  string
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

type GravitonStore struct {
	DB            *graviton.Store
	DBFolder      string
	DBPath        string
	DBTree        string
	migrating     int
	DBMaxSnapshot uint64
	DBMigrateWait time.Duration
	Writing       int
}

type TreeKV struct {
	k []byte
	v []byte
}

var Graviton_backend *GravitonStore = &GravitonStore{}
var StorageInfoLogger = logFileOutStorage("INFO")
var StorageErrorLogger = logFileOutStorage("ERROR")

func (g *GravitonStore) NewGravDB(poolhost, dbFolder, dbmigratewait string, dbmaxsnapshot uint64) {
	current_path, err := os.Getwd()
	if err != nil {
		log.Printf("%v", err)
	}

	g.DBMigrateWait, _ = time.ParseDuration(dbmigratewait)

	g.DBMaxSnapshot = dbmaxsnapshot

	g.DBFolder = dbFolder

	g.DBPath = filepath.Join(current_path, dbFolder)

	g.DB, _ = graviton.NewDiskStore(g.DBPath)

	g.DBTree = poolhost

	log.Printf("[Graviton] Initializing graviton store at path: %v", filepath.Join(current_path, dbFolder))
	StorageInfoLogger.Printf("[Graviton] Initializing graviton store at path: %v", filepath.Join(current_path, dbFolder))
}

// Swaps the store pointer from existing to new after copying latest snapshot to new DB - fast as cursor + disk writes allow [possible other alternatives such as mem store for some of these interwoven, testing needed]
func (g *GravitonStore) SwapGravDB(poolhost, dbFolder string) {
	// Use g.migrating as a simple 'mutex' of sorts to lock other read/write functions out of doing anything with DB until this function has completed.
	g.migrating = 1
	// Rename existing bak to bak2, then goroutine to cleanup so process doesn't wait for old db cleanup time
	var bakFolder string = dbFolder + "_bak"
	var bak2Folder string = dbFolder + "_bak2"
	log.Printf("Renaming directory %v to %v", bakFolder, bak2Folder)
	StorageInfoLogger.Printf("Renaming directory %v to %v", bakFolder, bak2Folder)
	os.Rename(bakFolder, bak2Folder)
	log.Printf("Removing directory %v", bak2Folder)
	StorageInfoLogger.Printf("Removing directory %v", bak2Folder)
	go os.RemoveAll(bak2Folder)

	// Get existing store values, defer close of original, and get store values for new DB to write to
	store := g.DB
	ss, _ := store.LoadSnapshot(0)
	tree, _ := ss.GetTree(g.DBTree)
	log.Printf("SS: %v", ss.GetVersion())

	c := tree.Cursor()
	log.Printf("Getting k/v pairs")
	StorageInfoLogger.Printf("Getting k/v pairs")
	// Duplicate the LATEST (snapshot 0) to the new DB, this starts the DB over again, but still retaining X number of old DBs for version in future use cases. Here we get the vals before swapping to new db in mem
	var treeKV []*TreeKV // Just k & v which are of type []byte
	for k, v, err := c.First(); err == nil; k, v, err = c.Next() {
		temp := &TreeKV{k, v}
		treeKV = append(treeKV, temp)
	}
	log.Printf("Closing store")
	StorageInfoLogger.Printf("Closing store")
	store.Close()

	// Backup last set of g.DBMaxSnapshot snapshots, can offload elsewhere or make this process as X many times as you want to backup.
	var oldFolder string
	oldFolder = g.DBPath
	log.Printf("Renaming directory %v to %v", oldFolder, bakFolder)
	StorageInfoLogger.Printf("Renaming directory %v to %v", oldFolder, bakFolder)
	os.Rename(oldFolder, bakFolder)

	log.Printf("Creating new disk store")
	StorageInfoLogger.Printf("Creating new disk store")
	g.DB, _ = graviton.NewDiskStore(g.DBPath)

	// Take vals from previous DB store that were put into treeKV struct (array of), and commit to new DB after putting all k/v pairs back
	store = g.DB
	ss, _ = store.LoadSnapshot(0)
	tree, _ = ss.GetTree(g.DBTree)

	log.Printf("Putting k/v pairs into tree...")
	StorageInfoLogger.Printf("Putting k/v pairs into tree...")
	for _, val := range treeKV {
		tree.Put(val.k, val.v)
	}
	log.Printf("Committing k/v pairs to tree")
	StorageInfoLogger.Printf("Committing k/v pairs to tree")
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	log.Printf("Migration to new DB is done.")
	StorageInfoLogger.Printf("Migration to new DB is done.")
	g.migrating = 0
}

func (g *GravitonStore) WriteBlocks(info *BlockDataGrav, blockType string) error {
	log.Printf("[Graviton] Inputting info: %v", info)
	StorageInfoLogger.Printf("[Graviton] Inputting info: %v", info)
	confBytes, err := json.Marshal(info)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal minedblocks info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal minedblocks info: %v", err)
	}

	// Store blocks found heights - only on candidate / found blocks by miners
	if blockType == "candidate" {
		err = g.WriteBlocksFoundByHeightArr(info.Height, info.Solo)
		if err != nil {
			log.Printf("[Graviton] Error writing blocksfoundbyheightarr: %v", err)
			StorageErrorLogger.Printf("[Graviton] Error writing blocksfoundbyheightarr: %v", err)
		}
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WriteBlocks] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WriteBlocks] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config

	// Store non-matured blocks (candidates, orphaned, immature etc.) normally with key block:blocktype:height, matured within an array of matured blocks for db growth & k/v growth mgmt
	if blockType != "matured" {
		key := "block:" + blockType + ":" + strconv.FormatInt(info.Height, 10)
		tree.Put([]byte(key), []byte(confBytes)) // insert a value
	} else {
		key := "block:matured"
		currMaturedBlocks, err := tree.Get([]byte(key))
		var maturedBlocks *BlocksFound

		var newMaturedBlocks []byte

		if err != nil {
			// Returns key not found if != nil, or other err, but assuming keynotfound/leafnotfound
			var maturedBlocksArr []*BlockDataGrav
			maturedBlocksArr = append(maturedBlocksArr, info)
			maturedBlocks = &BlocksFound{MinedBlocks: maturedBlocksArr}
		} else {
			// Retrieve value and convert to BlocksFoundByHeight, so that you can manipulate and update db
			log.Printf("Appending block. Stored []byte maturedblocks slice size: %v", len(currMaturedBlocks))
			StorageInfoLogger.Printf("Appending block. Stored []byte maturedblocks slice size: %v", len(currMaturedBlocks))
			_ = json.Unmarshal(currMaturedBlocks, &maturedBlocks)

			maturedBlocks.MinedBlocks = append(maturedBlocks.MinedBlocks, info)
			log.Printf("Appending block. MaturedBlock slice size: %v", len(maturedBlocks.MinedBlocks))
			StorageInfoLogger.Printf("Appending block. MaturedBlock slice size: %v", len(maturedBlocks.MinedBlocks))
		}
		newMaturedBlocks, err = json.Marshal(maturedBlocks)
		if err != nil {
			StorageErrorLogger.Printf("[Graviton] could not marshal maturedBlocks info: %v", err)
			return fmt.Errorf("[Graviton] could not marshal maturedBlocks info: %v", err)
		}

		// TODO: max value size of 104857600 would come out to ~209,715 blocks [~[500]byte or so a piece] capable of being stored this way before errs on value store and need for splitting.
		tree.Put([]byte(key), newMaturedBlocks)
	}

	// Remove blocks from previous rounds (orphaned removes candidate, immature removes candidate, matured removes immature)
	switch blockType {
	case "orphaned":
		key := "block:" + "candidate" + ":" + strconv.FormatInt(info.Height, 10)

		log.Printf("[Graviton] Removing info: %v", key)
		StorageInfoLogger.Printf("[Graviton] Removing info: %v", key)
		err := tree.Delete([]byte(key))
		if err != nil {
			return err
		}
	case "immature":
		key := "block:" + "candidate" + ":" + strconv.FormatInt(info.Height, 10)

		log.Printf("[Graviton] Removing info: %v", key)
		StorageInfoLogger.Printf("[Graviton] Removing info: %v", key)
		err := tree.Delete([]byte(key))
		if err != nil {
			return err
		}
	case "matured":
		key := "block:" + "immature" + ":" + strconv.FormatInt(info.Height, 10)

		log.Printf("[Graviton] Removing info: %v", key)
		StorageInfoLogger.Printf("[Graviton] Removing info: %v", key)
		err := tree.Delete([]byte(key))
		if err != nil {
			return err
		}
	}
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

// Array of int64 [heights] of blocks found by pool, this does not include solo blocks found. Used as reference points for round hash calculations
func (g *GravitonStore) WriteBlocksFoundByHeightArr(height int64, isSolo bool) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WriteBlocksFoundByHeightArr] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WriteBlocksFoundByHeightArr] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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
		StorageErrorLogger.Printf("[Graviton] could not marshal foundByHeight info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal foundByHeight info: %v", err)
	}

	tree.Put([]byte(key), newFoundByHeight)

	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) GetBlocksFoundByHeightArr() *BlocksFoundByHeight {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetBlocksFoundByHeightArr] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetBlocksFoundByHeightArr] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetBlocksFound] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetBlocksFound] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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
	}

	// Matured. This is outside of the previous loop since all matured are stored as a slice array while the others are individually stored (and removed)
	if blocktype == "matured" || blocktype == "all" {
		key := "block:matured"
		v, _ := tree.Get([]byte(key))
		if v != nil {
			var reply *BlocksFound
			_ = json.Unmarshal(v, &reply)

			for _, v := range reply.MinedBlocks {
				blocksFound.MinedBlocks = append(blocksFound.MinedBlocks, v)
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
	// TODO: Configure share tracking for solo as well? For effort calcs
	if !block.Solo {
		_, totalShares, _ := g.GetRoundShares(block.Height)
		immatureBlock.TotalShares = totalShares
	}

	err := g.WriteBlocks(immatureBlock, "immature")
	if err != nil {
		log.Printf("[Graviton] Error when adding immature block store at height %v: %v", immatureBlock.Height, err)
		StorageErrorLogger.Printf("[Graviton] Error when adding immature block store at height %v: %v", immatureBlock.Height, err)
	}

	return nil
}

func (g *GravitonStore) WriteMaturedBlocks(block *BlockDataGrav) error {
	// Add to matured store
	maturedBlock := block
	maturedBlock.BlockState = "matured"

	// If block is not solo, set totalShares.
	// TODO: Configure share tracking for solo as well? For effort calcs
	if !block.Solo {
		_, totalShares, _ := g.GetRoundShares(block.Height)
		maturedBlock.TotalShares = totalShares
	}

	err := g.WriteBlocks(maturedBlock, "matured")
	if err != nil {
		log.Printf("[Graviton] Error when adding matured block store at height %v: %v", maturedBlock.Height, err)
		StorageErrorLogger.Printf("[Graviton] Error when adding matured block store at height %v: %v", maturedBlock.Height, err)
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
			StorageErrorLogger.Printf("[Graviton] Error when adding orphaned block store at height %v: %v", value.Height, err)
			break
		}
	}

	return nil
}

// Function that will remove a k/v pair
func (g *GravitonStore) RemoveKey(key string) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[RemoveKey] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[RemoveKey] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config

	log.Printf("[Graviton] Removing info: %v", key)
	StorageInfoLogger.Printf("[Graviton] Removing info: %v", key)
	err := tree.Delete([]byte(key))
	if err != nil {
		StorageErrorLogger.Printf("%v", err)
		return err
	}
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) WriteImmaturePayments(info *PaymentPending) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WriteImmaturePayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WriteImmaturePayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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
		StorageErrorLogger.Printf("[Graviton] could not marshal paymentsPending info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal paymentsPending info: %v", err)
	}

	tree.Put([]byte(key), newPaymentsPending)
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	/*
		v, err := tree.Get([]byte(key))
		if v != nil {
			var reply *PendingPayments
			_ = json.Unmarshal(v, &reply)
		}
	*/

	return nil
}

func (g *GravitonStore) WritePendingPayments(info *PaymentPending) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WritePendingPayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WritePendingPayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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
				StorageInfoLogger.Printf("[Graviton] Updating value for %v from %v to %v", info.Address, paymentsPending.PendingPayout[p].Amount, (paymentsPending.PendingPayout[p].Amount + info.Amount))
				paymentsPending.PendingPayout[p].Amount += info.Amount
				updateExisting = true
			}
		}

		// If an existing payment was not upated since the addresses didn't match, append the new payment
		if !updateExisting {
			log.Printf("[Graviton] Appending new payment: %v", info)
			StorageInfoLogger.Printf("[Graviton] Appending new payment: %v", info)
			paymentsPending.PendingPayout = append(paymentsPending.PendingPayout, info)
		}
	}
	newPaymentsPending, err = json.Marshal(paymentsPending)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal paymentsPending info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal paymentsPending info: %v", err)
	}

	tree.Put([]byte(key), newPaymentsPending)
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	/*
		v, err := tree.Get([]byte(key))
		if v != nil {
			var reply *PendingPayments
			_ = json.Unmarshal(v, &reply)
		}
	*/

	return nil
}

func (g *GravitonStore) GetPendingPayments() []*PaymentPending {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetPendingPayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetPendingPayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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
		StorageErrorLogger.Printf("[Graviton] could not marshal pendingpayments info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal pendingpayments info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[OverwritePendingPayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[OverwritePendingPayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "payments:pending"

	tree.Put([]byte(key), confBytes)
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) WriteProcessedPayments(info *MinerPayments) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WriteProcessedPayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WriteProcessedPayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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
		log.Printf("Appending payment processed. Stored []byte minerpayments slice size: %v", len(currPaymentsProcessed))
		StorageInfoLogger.Printf("Appending payment processed. Stored []byte minerpayments slice size: %v", len(currPaymentsProcessed))
		_ = json.Unmarshal(currPaymentsProcessed, &paymentsProcessed)

		paymentsProcessed.MinerPayments = append(paymentsProcessed.MinerPayments, info)
		log.Printf("Appending payment processed. MinerPayments slice size: %v", len(paymentsProcessed.MinerPayments))
		StorageInfoLogger.Printf("Appending payment processed. MinerPayments slice size: %v", len(paymentsProcessed.MinerPayments))
	}
	newPaymentsProcessed, err = json.Marshal(paymentsProcessed)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal paymentsProcessed info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal paymentsProcessed info: %v", err)
	}

	tree.Put([]byte(key), newPaymentsProcessed)
	// TODO: max value size of 104857600 would come out to ~299,593 payments [~[350]byte or so a piece] capable of being stored this way before errs on value store and need for splitting.
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) GetProcessedPayments() *ProcessedPayments {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetProcessedPayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetProcessedPayments] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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

func (g *GravitonStore) WriteConfig(config *pool.Config) error {
	confBytes, err := json.Marshal(config)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal pool.Config info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal pool.Config info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WriteConfig] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WriteConfig] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "config:" + config.Coin
	tree.Put([]byte(key), []byte(confBytes)) // insert a value
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) GetConfig(coin string) *pool.Config {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetConfig] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetConfig] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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
	StorageInfoLogger.Printf("[Graviton] Registering miner: %v", miner.Id)
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WriteMinerIDRegistration] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WriteMinerIDRegistration] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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
				StorageInfoLogger.Printf("[Graviton] Miner already registered: %v", miner.Id)
				return nil
			}
		}

		minerIDs.Miners = append(minerIDs.Miners, miner)
	}

	// Since we know the miner is not already registered [would have returned out above if it were], we can store into miner stats to prep stats for later
	confMiner, err := json.Marshal(miner)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal miner info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal miner info: %v", err)
	}
	mk := "miners:stats:" + miner.Id
	tree.Put([]byte(mk), confMiner)

	newMinerIDs, err = json.Marshal(minerIDs)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal minerIDs info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal minerIDs info: %v", err)
	}

	tree.Put([]byte(key), newMinerIDs)
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) GetMinerIDRegistrations() []*Miner {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetMinerIDRegistrations] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetMinerIDRegistrations] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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

func (g *GravitonStore) CompareMinerStats(storedMiner, miner *Miner, hashrateExpiration time.Duration) (*Miner, bool) {
	// Get existing, compare the roundShares of each...
	// Check online etc.
	// Compare storedMiner to miner input, update the stored miner with a few 'appends'
	updatedMiner := miner

	if storedMiner != nil {
		if updatedMiner != nil {
			// Sync donationtotal for all-time stats
			if atomic.LoadInt64(&storedMiner.DonationTotal) >= atomic.LoadInt64(&updatedMiner.DonationTotal) {
				diff := atomic.LoadInt64(&storedMiner.DonationTotal) - atomic.LoadInt64(&updatedMiner.DonationTotal)
				if diff < 0 {
					diff = 0
				}
				updatedMiner.Lock()
				atomic.AddInt64(&updatedMiner.DonationTotal, diff)
				updatedMiner.Unlock()
			}

			// Sync accepts for all-time stats
			if atomic.LoadInt64(&storedMiner.Accepts) >= atomic.LoadInt64(&updatedMiner.Accepts) {
				diff := atomic.LoadInt64(&storedMiner.Accepts) - atomic.LoadInt64(&updatedMiner.Accepts)
				if diff < 0 {
					diff = 0
				}
				updatedMiner.Lock()
				atomic.AddInt64(&updatedMiner.Accepts, diff)
				updatedMiner.Unlock()
			}

			// Sync rejects for all-time stats
			if atomic.LoadInt64(&storedMiner.Rejects) >= atomic.LoadInt64(&updatedMiner.Rejects) {
				diff := atomic.LoadInt64(&storedMiner.Rejects) - atomic.LoadInt64(&updatedMiner.Rejects)
				if diff < 0 {
					diff = 0
				}
				updatedMiner.Lock()
				atomic.AddInt64(&updatedMiner.Rejects, diff)
				updatedMiner.Unlock()
			}
		}
	}

	var newMiner *Miner
	if updatedMiner != nil {
		newMiner = updatedMiner
		return newMiner, true
	} else {
		return storedMiner, false
	}
}

func (g *GravitonStore) WriteMinerStats(miners MinersMap, hashrateExpiration time.Duration) error {
	var confBytes []byte
	var err error
	var Commit bool
	storedMinerSlice := g.GetAllMinerStats()

	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WriteMinerStats] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WriteMinerStats] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config

	// If storedMinerMap is empty, set it to miners
	if storedMinerSlice != nil {
		for _, storedMiner := range storedMinerSlice {
			currMiner, _ := miners.Get(storedMiner.Id)
			updatedMiner, changes := g.CompareMinerStats(storedMiner, currMiner, hashrateExpiration)

			// Set the mmap object of the updated miner
			miners.Set(storedMiner.Id, updatedMiner)

			// Sometimes can run into concurrent read/write with updatedMiner. Possible misuse of same memory space, investigate further through testing might be required.]
			// Initial thought is the memory index of the map for shares is still linked back to in-use miner struct on each share submission, however that's just used to calc hashrate, so np missing one
			// This will only impact 1 block at a time, as roundShares would go to 0 immediately following the submission.
			if changes {
				Commit = true
				updatedMiner.Lock()
				confBytes, err = json.Marshal(updatedMiner)
				updatedMiner.Unlock()
				if err != nil {
					StorageErrorLogger.Printf("[Graviton] could not marshal miner stats: %v", err)
					return fmt.Errorf("[Graviton] could not marshal miner stats: %v", err)
				}

				key := "miners:stats:" + updatedMiner.Id // TODO: Append on the miner ID
				tree.Put([]byte(key), []byte(confBytes)) // insert a value
			}
		}
	} else {
		registeredMiners := g.GetMinerIDRegistrations()

		for _, value := range registeredMiners {
			currMiner, _ := miners.Get(value.Id)

			if currMiner != nil {
				confBytes, err = json.Marshal(currMiner)
				if err != nil {
					StorageErrorLogger.Printf("[Graviton] could not marshal miner stats: %v", err)
					return fmt.Errorf("[Graviton] could not marshal miner stats: %v", err)
				}

				key := "miners:stats:" + currMiner.Id    // TODO: Append on the miner ID
				tree.Put([]byte(key), []byte(confBytes)) // insert a value
			}
		}
	}
	if Commit {
		_, cerr := graviton.Commit(tree)
		if cerr != nil {
			log.Printf("[Graviton] ERROR: %v", cerr)
			StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
		}
	}

	return nil
}

func (g *GravitonStore) WriteMinerStatsByID(miner *Miner, hashrateExpiration time.Duration) error {
	storedMiner := g.GetMinerStatsByID(miner.Id)

	updatedMiner, _ := g.CompareMinerStats(storedMiner, miner, hashrateExpiration)

	confBytes, err := json.Marshal(updatedMiner)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal updated miner info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal updated miner info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WriteMinerStatsByID] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WriteMinerStatsByID] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "miners:stats:" + updatedMiner.Id
	tree.Put([]byte(key), []byte(confBytes)) // insert a value
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) GetAllMinerStats() []*Miner {
	var allMiners []*Miner
	registeredMiners := g.GetMinerIDRegistrations()

	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetAllMinerStats] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetAllMinerStats] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetMinerStatsByID] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetMinerStatsByID] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetRoundShares] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetRoundShares] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

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

func (g *GravitonStore) UpdatePoolRoundStats(miners MinersMap) error {
	storedMinerSlice := g.GetAllMinerStats()
	poolRoundStats := g.GetPoolRoundStats()
	candidatePoolBlocksFound := g.GetBlocksFound("candidate")
	immaturePoolBlocksFound := g.GetBlocksFound("immature")
	maturePoolBlocksFound := g.GetBlocksFound("matured")

	currentPoolRoundStats := &PoolRound{StartTimestamp: int64(0), Timestamp: int64(0), RoundShares: make(map[string]int64), LastBlockHeight: int64(0)}
	var referenceBlock *BlockDataGrav

	// Create slice of heights that do not include solo blocks. This will be used to compare the last block found against miner heights below
	blockHeightArr := g.GetBlocksFoundByHeightArr()

	var heights []int64
	if blockHeightArr != nil {
		for height, isSolo := range blockHeightArr.Heights {
			if !isSolo {
				heights = append(heights, height)
			}
		}
		// Sort heights so most recent is index 0 [if preferred reverse, just swap > with <]
		sort.SliceStable(heights, func(i, j int) bool {
			return heights[i] > heights[j]
		})
	}

	// If heights length is > 1 then block(s) exist. Get the latest block from candidate/immature/matured variables [annoying procedure, however don't currently have a getBlock(x height) - TODO]
	if len(heights) >= 1 {
		for _, value := range candidatePoolBlocksFound.MinedBlocks {
			if value.Height == heights[0] {
				referenceBlock = value
				break
			}
		}

		// Ensure referenceBlock hadn't already been found to save cycles
		if referenceBlock != nil {
			for _, value := range immaturePoolBlocksFound.MinedBlocks {
				if value.Height == heights[0] {
					referenceBlock = value
					break
				}
			}
		}

		// Ensure referenceBlock hadn't already been found to save cycles
		if referenceBlock != nil {
			for _, value := range maturePoolBlocksFound.MinedBlocks {
				if value.Height == heights[0] {
					referenceBlock = value
					break
				}
			}
		}
	}

	now := (time.Now().UnixNano() / int64(time.Millisecond)) / 1000

	// We need to define StartTimestamp and Timestamp
	var nextRound bool
	if poolRoundStats != nil {
		if referenceBlock != nil {
			if poolRoundStats.StartTimestamp < referenceBlock.Timestamp || poolRoundStats.LastBlockHeight < referenceBlock.Height {
				nextRound = true
				currentPoolRoundStats.StartTimestamp = poolRoundStats.Timestamp
				currentPoolRoundStats.Timestamp = referenceBlock.Timestamp
				currentPoolRoundStats.RoundShares = poolRoundStats.RoundShares
				currentPoolRoundStats.LastBlockHeight = referenceBlock.Height
			} else {
				currentPoolRoundStats.StartTimestamp = poolRoundStats.Timestamp
				currentPoolRoundStats.Timestamp = now
				currentPoolRoundStats.RoundShares = poolRoundStats.RoundShares
				currentPoolRoundStats.LastBlockHeight = poolRoundStats.LastBlockHeight
			}
		} else {
			currentPoolRoundStats.StartTimestamp = poolRoundStats.Timestamp
			currentPoolRoundStats.Timestamp = now
			currentPoolRoundStats.RoundShares = poolRoundStats.RoundShares
		}
	} else {
		if referenceBlock != nil {
			currentPoolRoundStats.StartTimestamp = referenceBlock.Timestamp
			currentPoolRoundStats.Timestamp = now
			currentPoolRoundStats.RoundShares = make(map[string]int64)
			currentPoolRoundStats.LastBlockHeight = referenceBlock.Height
		} else {
			currentPoolRoundStats.StartTimestamp = 0
			currentPoolRoundStats.Timestamp = now
			currentPoolRoundStats.RoundShares = make(map[string]int64)
		}
	}

	// If storedMinerMap is empty, no round stats to add
	if storedMinerSlice != nil {
		for _, storedMiner := range storedMinerSlice {
			currMiner, ok := miners.Get(storedMiner.Id)

			if ok {
				for k, v := range currMiner.Shares {
					if k > currentPoolRoundStats.StartTimestamp && k <= currentPoolRoundStats.Timestamp {
						currentPoolRoundStats.RoundShares[storedMiner.Id] += v
					}
				}
			} else {
				//log.Printf("[UpdatePoolRoundStats] No active miner under %v, no need to update roundshares from this miner.", storedMiner.Id)
				//StorageInfoLogger.Printf("[UpdatePoolRoundStats] No active miner under %v, no need to update roundshares from this miner.", storedMiner.Id)
			}
		}
	}

	if nextRound {
		// If nextRound is triggered, we clear the stored pool stats and then we store nextRound details
		log.Printf("[UpdatePoolRoundStats] Starting next round...")
		StorageInfoLogger.Printf("[UpdatePoolRoundStats] Starting next round...")
		log.Printf("[UpdatePoolRoundStats] Storing previous round: RoundShares (%v) , Height (%v)", currentPoolRoundStats.RoundShares, referenceBlock.Height)
		StorageInfoLogger.Printf("[UpdatePoolRoundStats] Storing previous round: RoundShares (%v) , Height (%v)", currentPoolRoundStats.RoundShares, referenceBlock.Height)

		g.WriteRoundShares(referenceBlock.Height, currentPoolRoundStats.RoundShares)

		log.Printf("[UpdatePoolRoundStats] Clearing out stored values and storing clean roundshares object")
		StorageInfoLogger.Printf("[UpdatePoolRoundStats] Clearing out stored values and storing clean roundshares object")
		currentPoolRoundStats.StartTimestamp = referenceBlock.Timestamp
		currentPoolRoundStats.Timestamp = referenceBlock.Timestamp
		currentPoolRoundStats.RoundShares = make(map[string]int64)
	}

	err := g.OverwritePoolRoundStats(currentPoolRoundStats)
	if err != nil {
		log.Printf("[UpdatePoolRoundStats] Err on overwriting pool stats: %v", err)
		StorageErrorLogger.Printf("[UpdatePoolRoundStats] Err on overwriting pool stats: %v", err)
	} else {
		if nextRound {
			log.Printf("[UpdatePoolRoundStats] Updated pool round stats for next round.")
			StorageInfoLogger.Printf("[UpdatePoolRoundStats] Updated pool round stats for next round.")
		} else {
			log.Printf("[UpdatePoolRoundStats] Updated pool round stats.")
			StorageInfoLogger.Printf("[UpdatePoolRoundStats] Updated pool round stats.")
		}
	}

	return nil
}

// This function is to overwrite pool round stats which will retain *current* pool round details and updated to blank out at each new round / nextRound storage function
func (g *GravitonStore) OverwritePoolRoundStats(info *PoolRound) error {
	confBytes, err := json.Marshal(info)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal pendingpayments info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal pendingpayments info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[OverwritePoolRoundStats] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[OverwritePoolRoundStats] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "pool:currentround"

	tree.Put([]byte(key), confBytes)
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) GetPoolRoundStats() *PoolRound {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetPoolRoundStats] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetPoolRoundStats] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "pool:currentround"

	var result *PoolRound

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &result)
	}

	return result
}

func (g *GravitonStore) WriteRoundShares(roundHeight int64, roundShares map[string]int64) error {
	confBytes, err := json.Marshal(roundShares)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal roundShares info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal roundShares info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WriteRoundShares] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WriteRoundShares] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "miners:round:" + strconv.FormatInt(roundHeight, 10)
	log.Printf("[Graviton-WriteRoundShares] Storing %v with values: %v", key, roundShares)
	StorageInfoLogger.Printf("[Graviton-WriteRoundShares] Storing %v with values: %v", key, roundShares)
	tree.Put([]byte(key), []byte(confBytes)) // insert a value
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) WriteChartsData(data *ChartData, chartType string, interval, maximumPeriod int64) error {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WriteChartsData] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WriteChartsData] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	chartWait, _ := time.ParseDuration("100ms")
	time.Sleep(chartWait)

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "charts:" + chartType
	currCharts, err := tree.Get([]byte(key))
	var charts *GravitonCharts

	var newChartData []byte

	if err != nil {
		// Returns key not found if != nil, or other err, but assuming keynotfound/leafnotfound
		var chartArr []*ChartData
		chartArr = append(chartArr, data)
		charts = &GravitonCharts{Values: chartArr}
	} else {
		// Retrieve value and convert to charts, so that you can manipulate and update db
		_ = json.Unmarshal(currCharts, &charts)

		for _, value := range charts.Values {
			if value.Timestamp == data.Timestamp {
				log.Printf("[Graviton] Chart timestamp already registered: %v", data.Timestamp)
				//StorageInfoLogger.Printf("[Graviton] Miner already registered: %v", miner.Id)
				return nil
			}
		}

		charts.Values = append(charts.Values, data)

		// Sort charts so most recent is index 0 [if preferred reverse, just swap > with <]
		sort.SliceStable(charts.Values, func(i, j int) bool {
			return charts.Values[i].Timestamp > charts.Values[j].Timestamp
		})

		// Trim off the end if .Values has more than defined maximumPeriod of chart data
		maxNumValues := maximumPeriod / interval

		if int64(len(charts.Values)) >= maxNumValues {
			// Truncate slice to be to maxNumValues
			charts.Values = charts.Values[:maxNumValues]
		}
	}

	newChartData, err = json.Marshal(charts)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal charts info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal charts info: %v", err)
	}

	tree.Put([]byte(key), newChartData)
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) GetChartsData(chartType string) *GravitonCharts {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetChartsData] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetChartsData] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "charts:" + chartType

	var result *GravitonCharts

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &result)
	}

	return result
}

// This function is to overwrite events data
func (g *GravitonStore) OverwriteEventsData(info map[string]*Miner, date string) error {
	confBytes, err := json.Marshal(info)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal eventsdata info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal eventsdata info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[OverwriteEventsData] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[OverwriteEventsData] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "events:" + date

	tree.Put([]byte(key), confBytes)
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) GetEventsData(date string) map[string]*Miner {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetEventsData] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetEventsData] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "events:" + date

	var result map[string]*Miner

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &result)
	}

	return result
}

func (g *GravitonStore) WriteEventsPayment(payment *PaymentPending, date string) error {
	confBytes, err := json.Marshal(payment)
	if err != nil {
		StorageErrorLogger.Printf("[Graviton] could not marshal eventsdata info: %v", err)
		return fmt.Errorf("[Graviton] could not marshal eventsdata info: %v", err)
	}

	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[WriteEventsPayment] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[WriteEventsPayment] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "eventspayment:" + date

	tree.Put([]byte(key), confBytes)
	_, cerr := graviton.Commit(tree)
	if cerr != nil {
		log.Printf("[Graviton] ERROR: %v", cerr)
		StorageErrorLogger.Printf("[Graviton] ERROR: %v", cerr)
	}
	return nil
}

func (g *GravitonStore) GetEventsPayment(date string) *PaymentPending {
	store := g.DB
	ss, _ := store.LoadSnapshot(0) // load most recent snapshot

	// Swap DB at g.DBMaxSnapshot+ commits. Check for g.migrating, if so sleep for g.DBMigrateWait ms
	for g.migrating == 1 {
		log.Printf("[GetEventsData] G is migrating... sleeping for %v...", g.DBMigrateWait)
		StorageInfoLogger.Printf("[GetEventsData] G is migrating... sleeping for %v...", g.DBMigrateWait)
		time.Sleep(g.DBMigrateWait)
		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}
	if ss.GetVersion() >= g.DBMaxSnapshot {
		Graviton_backend.SwapGravDB(Graviton_backend.DBTree, Graviton_backend.DBFolder)

		store = g.DB
		ss, _ = store.LoadSnapshot(0) // load most recent snapshot
	}

	tree, _ := ss.GetTree(g.DBTree) // use or create tree named by poolhost in config
	key := "eventspayment:" + date

	var result *PaymentPending

	v, _ := tree.Get([]byte(key))
	if v != nil {
		_ = json.Unmarshal(v, &result)
	}

	return result
}

func logFileOutStorage(lType string) *log.Logger {
	var logFileName string
	if lType == "ERROR" {
		logFileName = "logs/storageError.log"
	} else {
		logFileName = "logs/storage.log"
	}
	os.Mkdir("logs", 0705)
	f, err := os.OpenFile(logFileName, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0705)
	if err != nil {
		panic(err)
	}

	logType := lType + ": "
	l := log.New(f, logType, log.LstdFlags|log.Lmicroseconds)
	return l
}
