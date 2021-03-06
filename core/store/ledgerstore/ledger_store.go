/*
 * Copyright (C) 2018 The ontology Authors
 * This file is part of The ontology library.
 *
 * The ontology is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology.  If not, see <http://www.gnu.org/licenses/>.
 */

//Storage of ledger
package ledgerstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ontio/ontology-crypto/keypair"
	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/config"
	"github.com/ontio/ontology/common/log"
	"github.com/ontio/ontology/common/serialization"
	"github.com/ontio/ontology/consensus/vbft/config"
	"github.com/ontio/ontology/core/chainmgr/xshard_state"
	"github.com/ontio/ontology/core/payload"
	"github.com/ontio/ontology/core/signature"
	"github.com/ontio/ontology/core/states"
	"github.com/ontio/ontology/core/store"
	scom "github.com/ontio/ontology/core/store/common"
	"github.com/ontio/ontology/core/store/overlaydb"
	"github.com/ontio/ontology/core/types"
	"github.com/ontio/ontology/core/xshard_types"
	"github.com/ontio/ontology/errors"
	"github.com/ontio/ontology/events"
	"github.com/ontio/ontology/events/message"
	"github.com/ontio/ontology/smartcontract"
	scommon "github.com/ontio/ontology/smartcontract/common"
	"github.com/ontio/ontology/smartcontract/event"
	"github.com/ontio/ontology/smartcontract/service/native/global_params"
	"github.com/ontio/ontology/smartcontract/service/native/shardmgmt"
	shardstates "github.com/ontio/ontology/smartcontract/service/native/shardmgmt/states"
	"github.com/ontio/ontology/smartcontract/service/native/utils"
	"github.com/ontio/ontology/smartcontract/service/neovm"
	sstate "github.com/ontio/ontology/smartcontract/states"
	"github.com/ontio/ontology/smartcontract/storage"
)

const (
	SYSTEM_VERSION          = byte(1)      //Version of ledger store
	HEADER_INDEX_BATCH_SIZE = uint32(2000) //Bath size of saving header index
)

var (
	//Storage save path.
	DBDirEvent          = "ledgerevent"
	DBDirBlock          = "block"
	DBDirState          = "states"
	DBDirBlockCache     = "blockCache"
	MerkleTreeStorePath = "merkle_tree.db"
)

//LedgerStoreImp is main store struct fo ledger
type LedgerStoreImp struct {
	parentShardStore     store.LedgerStore
	blockStore           *BlockStore                      //BlockStore for saving block & transaction data
	stateStore           *StateStore                      //StateStore for saving state data, like balance, smart contract execution result, and so on.
	eventStore           *EventStore                      //EventStore for saving log those gen after smart contract executed.
	storedIndexCount     uint32                           //record the count of have saved block index
	currBlockHeight      uint32                           //Current block height
	currBlockHash        common.Uint256                   //Current block hash
	headerCache          map[common.Uint256]*types.Header //BlockHash => Header
	headerIndex          map[uint32]common.Uint256        //Header index, Mapping header height => block hash
	savingBlockSemaphore chan bool
	vbftPeerInfoheader   map[string]uint32 //pubInfo save pubkey,peerindex
	vbftPeerInfoblock    map[string]uint32 //pubInfo save pubkey,peerindex
	lock                 sync.RWMutex
	stateHashCheckHeight uint32
}

//NewLedgerStore return LedgerStoreImp instance
func NewLedgerStore(dataDir string, stateHashHeight uint32, parentShardStore store.LedgerStore) (*LedgerStoreImp, error) {
	ledgerStore := &LedgerStoreImp{
		parentShardStore:     parentShardStore,
		headerIndex:          make(map[uint32]common.Uint256),
		headerCache:          make(map[common.Uint256]*types.Header, 0),
		vbftPeerInfoheader:   make(map[string]uint32),
		vbftPeerInfoblock:    make(map[string]uint32),
		savingBlockSemaphore: make(chan bool, 1),
		stateHashCheckHeight: stateHashHeight,
	}

	blockStore, err := NewBlockStore(fmt.Sprintf("%s%s%s", dataDir, string(os.PathSeparator), DBDirBlock), true)
	if err != nil {
		return nil, fmt.Errorf("NewBlockStore error %s", err)
	}
	ledgerStore.blockStore = blockStore

	dbPath := fmt.Sprintf("%s%s%s", dataDir, string(os.PathSeparator), DBDirState)
	merklePath := fmt.Sprintf("%s%s%s", dataDir, string(os.PathSeparator), MerkleTreeStorePath)
	stateStore, err := NewStateStore(dbPath, merklePath, stateHashHeight)
	if err != nil {
		return nil, fmt.Errorf("NewStateStore error %s", err)
	}
	ledgerStore.stateStore = stateStore

	eventState, err := NewEventStore(fmt.Sprintf("%s%s%s", dataDir, string(os.PathSeparator), DBDirEvent))
	if err != nil {
		return nil, fmt.Errorf("NewEventStore error %s", err)
	}
	ledgerStore.eventStore = eventState
	return ledgerStore, nil
}

//InitLedgerStoreWithGenesisBlock init the ledger store with genesis block. It's the first operation after NewLedgerStore.
func (this *LedgerStoreImp) InitLedgerStoreWithGenesisBlock(genesisBlock *types.Block, defaultBookkeeper []keypair.PublicKey) error {
	hasInit, err := this.hasAlreadyInitGenesisBlock()
	if err != nil {
		return fmt.Errorf("hasAlreadyInit error %s", err)
	}
	if !hasInit {
		err = this.blockStore.ClearAll()
		if err != nil {
			return fmt.Errorf("blockStore.ClearAll error %s", err)
		}
		err = this.stateStore.ClearAll()
		if err != nil {
			return fmt.Errorf("stateStore.ClearAll error %s", err)
		}
		err = this.eventStore.ClearAll()
		if err != nil {
			return fmt.Errorf("eventStore.ClearAll error %s", err)
		}

		defaultBookkeeper = keypair.SortPublicKeys(defaultBookkeeper)
		bookkeeperState := &states.BookkeeperState{
			CurrBookkeeper: defaultBookkeeper,
			NextBookkeeper: defaultBookkeeper,
		}
		err = this.stateStore.SaveBookkeeperState(bookkeeperState)
		if err != nil {
			return fmt.Errorf("SaveBookkeeperState error %s", err)
		}

		result, err := this.executeBlock(genesisBlock)
		if err != nil {
			return err
		}
		err = this.submitBlock(genesisBlock, result)
		if err != nil {
			return fmt.Errorf("save genesis block error %s", err)
		}
		err = this.initGenesisBlock()
		if err != nil {
			return fmt.Errorf("init error %s", err)
		}
		genHash := genesisBlock.Hash()
		log.Infof("GenesisBlock init success. GenesisBlock hash:%s\n", genHash.ToHexString())
	} else {
		err = this.init()
		if err != nil {
			return fmt.Errorf("init error %s", err)
		}
		if this.currBlockHeight == 0 {
			genesisHash := genesisBlock.Hash()
			exist, err := this.blockStore.ContainBlock(genesisHash)
			if err != nil {
				return fmt.Errorf("HashBlockExist error %s", err)
			}
			if !exist {
				return fmt.Errorf("GenesisBlock arenot init correctly, hash %s", genesisHash.ToHexString())
			}
		}
	}
	//load vbft peerInfo
	consensusType := strings.ToLower(config.DefConfig.Genesis.ConsensusType)
	if consensusType == "vbft" {
		header, err := this.GetHeaderByHash(this.currBlockHash)
		if err != nil {
			return err
		}
		blkInfo, err := vconfig.VbftBlock(header)
		if err != nil {
			return err
		}
		var cfg *vconfig.ChainConfig
		if blkInfo.NewChainConfig != nil {
			cfg = blkInfo.NewChainConfig
		} else {
			cfgHeader, err := this.GetHeaderByHeight(blkInfo.LastConfigBlockNum)
			if err != nil {
				return err
			}
			Info, err := vconfig.VbftBlock(cfgHeader)
			if err != nil {
				return err
			}
			if Info.NewChainConfig == nil {
				return fmt.Errorf("getNewChainConfig error block num:%d", blkInfo.LastConfigBlockNum)
			}
			cfg = Info.NewChainConfig
		}
		this.lock.Lock()
		this.vbftPeerInfoheader = make(map[string]uint32)
		this.vbftPeerInfoblock = make(map[string]uint32)
		for _, p := range cfg.Peers {
			this.vbftPeerInfoheader[p.ID] = p.Index
			this.vbftPeerInfoblock[p.ID] = p.Index
		}
		this.lock.Unlock()
	}
	// check and fix imcompatible states
	err = this.stateStore.CheckStorage()
	return err
}

func (this *LedgerStoreImp) hasAlreadyInitGenesisBlock() (bool, error) {
	version, err := this.blockStore.GetVersion()
	if err != nil && err != scom.ErrNotFound {
		return false, fmt.Errorf("GetVersion error %s", err)
	}
	return version == SYSTEM_VERSION, nil
}

func (this *LedgerStoreImp) initGenesisBlock() error {
	return this.blockStore.SaveVersion(SYSTEM_VERSION)
}

func (this *LedgerStoreImp) init() error {
	err := this.loadCurrentBlock()
	if err != nil {
		return fmt.Errorf("loadCurrentBlock error %s", err)
	}
	err = this.loadHeaderIndexList()
	if err != nil {
		return fmt.Errorf("loadHeaderIndexList error %s", err)
	}
	err = this.recoverStore()
	if err != nil {
		return fmt.Errorf("recoverStore error %s", err)
	}
	return nil
}

func (this *LedgerStoreImp) loadCurrentBlock() error {
	currentBlockHash, currentBlockHeight, err := this.blockStore.GetCurrentBlock()
	if err != nil {
		return fmt.Errorf("LoadCurrentBlock error %s", err)
	}
	log.Infof("InitCurrentBlock currentBlockHash %s currentBlockHeight %d", currentBlockHash.ToHexString(), currentBlockHeight)
	this.currBlockHash = currentBlockHash
	this.currBlockHeight = currentBlockHeight
	return nil
}

func (this *LedgerStoreImp) loadHeaderIndexList() error {
	currBlockHeight := this.GetCurrentBlockHeight()
	headerIndex, err := this.blockStore.GetHeaderIndexList()
	if err != nil {
		return fmt.Errorf("LoadHeaderIndexList error %s", err)
	}
	storeIndexCount := uint32(len(headerIndex))
	this.headerIndex = headerIndex
	this.storedIndexCount = storeIndexCount

	for i := storeIndexCount; i <= currBlockHeight; i++ {
		height := i
		blockHash, err := this.blockStore.GetBlockHash(height)
		if err != nil {
			return fmt.Errorf("LoadBlockHash height %d error %s", height, err)
		}
		if blockHash == common.UINT256_EMPTY {
			return fmt.Errorf("LoadBlockHash height %d hash nil", height)
		}
		this.headerIndex[height] = blockHash
	}
	return nil
}

func (this *LedgerStoreImp) recoverStore() error {
	blockHeight := this.GetCurrentBlockHeight()

	_, stateHeight, err := this.stateStore.GetCurrentBlock()
	if err != nil {
		return fmt.Errorf("stateStore.GetCurrentBlock error %s", err)
	}
	for i := stateHeight; i < blockHeight; i++ {
		blockHash, err := this.blockStore.GetBlockHash(i)
		if err != nil {
			return fmt.Errorf("blockStore.GetBlockHash height:%d error:%s", i, err)
		}
		block, err := this.blockStore.GetBlock(blockHash)
		if err != nil {
			return fmt.Errorf("blockStore.GetBlock height:%d error:%s", i, err)
		}
		this.eventStore.NewBatch()
		this.stateStore.NewBatch()
		result, err := this.executeBlock(block)
		if err != nil {
			return err
		}
		err = this.saveBlockToStateStore(block, result)
		if err != nil {
			return fmt.Errorf("save to state store height:%d error:%s", i, err)
		}
		err = this.saveBlockToEventStore(block)
		if err != nil {
			return fmt.Errorf("save to event store height:%d error:%s", i, err)
		}
		err = this.eventStore.CommitTo()
		if err != nil {
			return fmt.Errorf("eventStore.CommitTo height:%d error %s", i, err)
		}
		err = this.stateStore.CommitTo()
		if err != nil {
			return fmt.Errorf("stateStore.CommitTo height:%d error %s", i, err)
		}
	}
	return nil
}

func (this *LedgerStoreImp) setHeaderIndex(height uint32, blockHash common.Uint256) {
	this.lock.Lock()
	defer this.lock.Unlock()
	this.headerIndex[height] = blockHash
}

func (this *LedgerStoreImp) getHeaderIndex(height uint32) common.Uint256 {
	this.lock.RLock()
	defer this.lock.RUnlock()
	blockHash, ok := this.headerIndex[height]
	if !ok {
		return common.Uint256{}
	}
	return blockHash
}

//GetCurrentHeaderHeight return the current header height.
//In block sync states, Header height is usually higher than block height that is has already committed to storage
func (this *LedgerStoreImp) GetCurrentHeaderHeight() uint32 {
	this.lock.RLock()
	defer this.lock.RUnlock()
	size := len(this.headerIndex)
	if size == 0 {
		return 0
	}
	return uint32(size) - 1
}

//GetCurrentHeaderHash return the current header hash. The current header means the latest header.
func (this *LedgerStoreImp) GetCurrentHeaderHash() common.Uint256 {
	this.lock.RLock()
	defer this.lock.RUnlock()
	size := len(this.headerIndex)
	if size == 0 {
		return common.Uint256{}
	}
	return this.headerIndex[uint32(size)-1]
}

func (this *LedgerStoreImp) setCurrentBlock(height uint32, blockHash common.Uint256) {
	this.lock.Lock()
	defer this.lock.Unlock()
	this.currBlockHash = blockHash
	this.currBlockHeight = height
	return
}

//GetCurrentBlock return the current block height, and block hash.
//Current block means the latest block in store.
func (this *LedgerStoreImp) GetCurrentBlock() (uint32, common.Uint256) {
	this.lock.RLock()
	defer this.lock.RUnlock()
	return this.currBlockHeight, this.currBlockHash
}

//GetCurrentBlockHash return the current block hash
func (this *LedgerStoreImp) GetCurrentBlockHash() common.Uint256 {
	this.lock.RLock()
	defer this.lock.RUnlock()
	return this.currBlockHash
}

//GetCurrentBlockHeight return the current block height
func (this *LedgerStoreImp) GetCurrentBlockHeight() uint32 {
	this.lock.RLock()
	defer this.lock.RUnlock()
	return this.currBlockHeight
}

func (this *LedgerStoreImp) addHeaderCache(header *types.Header) {
	this.lock.Lock()
	defer this.lock.Unlock()
	this.headerCache[header.Hash()] = header
}

func (this *LedgerStoreImp) delHeaderCache(blockHash common.Uint256) {
	this.lock.Lock()
	defer this.lock.Unlock()
	delete(this.headerCache, blockHash)
}

func (this *LedgerStoreImp) getHeaderCache(blockHash common.Uint256) *types.Header {
	this.lock.RLock()
	defer this.lock.RUnlock()
	header, ok := this.headerCache[blockHash]
	if !ok {
		return nil
	}
	return header
}

func (this *LedgerStoreImp) verifyHeader(header *types.Header, vbftPeerInfo map[string]uint32) (map[string]uint32, error) {
	if header.Height == 0 {
		return vbftPeerInfo, nil
	}
	var prevHeader *types.Header
	prevHeaderHash := header.PrevBlockHash
	prevHeader, err := this.GetHeaderByHash(prevHeaderHash)
	if err != nil && err != scom.ErrNotFound {
		return vbftPeerInfo, fmt.Errorf("get prev header error %s", err)
	}
	if prevHeader == nil {
		return vbftPeerInfo, fmt.Errorf("cannot find pre header by blockHash %s", prevHeaderHash.ToHexString())
	}

	if prevHeader.Height+1 != header.Height {
		return vbftPeerInfo, fmt.Errorf("block height is incorrect")
	}

	if prevHeader.Timestamp >= header.Timestamp {
		return vbftPeerInfo, fmt.Errorf("block timestamp is incorrect")
	}
	consensusType := strings.ToLower(config.DefConfig.Genesis.ConsensusType)
	if consensusType == "vbft" {
		//check bookkeeppers
		m := len(vbftPeerInfo) - (len(vbftPeerInfo)*6)/7
		if len(header.Bookkeepers) < m {
			return vbftPeerInfo, fmt.Errorf("header Bookkeepers %d more than 6/7 len vbftPeerInfo%d", len(header.Bookkeepers), len(vbftPeerInfo))
		}
		for _, bookkeeper := range header.Bookkeepers {
			pubkey := vconfig.PubkeyID(bookkeeper)
			_, present := vbftPeerInfo[pubkey]
			if !present {
				log.Errorf("invalid pubkey :%v,height:%d", pubkey, header.Height)
				return vbftPeerInfo, fmt.Errorf("invalid pubkey :%v", pubkey)
			}
		}
		hash := header.Hash()
		err = signature.VerifyMultiSignature(hash[:], header.Bookkeepers, m, header.SigData)
		if err != nil {
			log.Errorf("VerifyMultiSignature:%s,Bookkeepers:%d,pubkey:%d,heigh:%d", err, len(header.Bookkeepers), len(vbftPeerInfo), header.Height)
			return vbftPeerInfo, err
		}
		blkInfo, err := vconfig.VbftBlock(header)
		if err != nil {
			return vbftPeerInfo, err
		}
		if blkInfo.NewChainConfig != nil {
			peerInfo := make(map[string]uint32)
			for _, p := range blkInfo.NewChainConfig.Peers {
				peerInfo[p.ID] = p.Index
			}
			return peerInfo, nil
		}
		return vbftPeerInfo, nil
	} else {
		address, err := types.AddressFromBookkeepers(header.Bookkeepers)
		if err != nil {
			return vbftPeerInfo, err
		}
		if prevHeader.NextBookkeeper != address {
			return vbftPeerInfo, fmt.Errorf("bookkeeper address error")
		}

		m := len(header.Bookkeepers) - (len(header.Bookkeepers)-1)/3
		hash := header.Hash()
		err = signature.VerifyMultiSignature(hash[:], header.Bookkeepers, m, header.SigData)
		if err != nil {
			return vbftPeerInfo, err
		}
	}
	return vbftPeerInfo, nil
}

//AddHeader add header to cache, and add the mapping of block height to block hash. Using in block sync
func (this *LedgerStoreImp) AddHeader(header *types.Header) error {
	nextHeaderHeight := this.GetCurrentHeaderHeight() + 1
	if header.Height != nextHeaderHeight {
		return fmt.Errorf("header height %d not equal next header height %d", header.Height, nextHeaderHeight)
	}
	var err error
	this.vbftPeerInfoheader, err = this.verifyHeader(header, this.vbftPeerInfoheader)
	if err != nil {
		return fmt.Errorf("verifyHeader error %s", err)
	}
	this.addHeaderCache(header)
	this.setHeaderIndex(header.Height, header.Hash())
	return nil
}

//AddHeaders bath add header.
func (this *LedgerStoreImp) AddHeaders(headers []*types.Header) error {
	sort.Slice(headers, func(i, j int) bool {
		return headers[i].Height < headers[j].Height
	})
	var err error
	for _, header := range headers {
		err = this.AddHeader(header)
		if err != nil {
			return err
		}
	}
	return nil
}

func (this *LedgerStoreImp) GetStateMerkleRoot(height uint32) (common.Uint256, error) {
	return this.stateStore.GetStateMerkleRoot(height)
}

func (this *LedgerStoreImp) ExecuteBlock(block *types.Block) (result store.ExecuteResult, err error) {
	this.getSavingBlockLock()
	defer this.releaseSavingBlockLock()
	currBlockHeight := this.GetCurrentBlockHeight()
	blockHeight := block.Header.Height
	if blockHeight <= currBlockHeight {
		result.MerkleRoot, err = this.GetStateMerkleRoot(blockHeight)
		return
	}
	nextBlockHeight := currBlockHeight + 1
	if blockHeight != nextBlockHeight {
		err = fmt.Errorf("block height %d not equal next block height %d", blockHeight, nextBlockHeight)
		return
	}

	result, err = this.executeBlock(block)
	return
}

func (this *LedgerStoreImp) SubmitBlock(block *types.Block, result store.ExecuteResult) error {
	this.getSavingBlockLock()
	defer this.releaseSavingBlockLock()
	currBlockHeight := this.GetCurrentBlockHeight()
	blockHeight := block.Header.Height
	if blockHeight <= currBlockHeight {
		return nil
	}
	nextBlockHeight := currBlockHeight + 1
	if blockHeight != nextBlockHeight {
		return fmt.Errorf("block height %d not equal next block height %d", blockHeight, nextBlockHeight)
	}
	var err error
	this.vbftPeerInfoblock, err = this.verifyHeader(block.Header, this.vbftPeerInfoblock)
	if err != nil {
		return fmt.Errorf("verifyHeader error %s", err)
	}

	err = this.submitBlock(block, result)
	if err != nil {
		return fmt.Errorf("saveBlock error %s", err)
	}
	this.delHeaderCache(block.Hash())
	return nil
}

func (this *LedgerStoreImp) saveBlockToBlockStore(block *types.Block) error {
	blockHash := block.Hash()
	blockHeight := block.Header.Height

	this.setHeaderIndex(blockHeight, blockHash)
	err := this.saveHeaderIndexList()
	if err != nil {
		return fmt.Errorf("saveHeaderIndexList error %s", err)
	}
	err = this.blockStore.SaveCurrentBlock(blockHeight, blockHash)
	if err != nil {
		return fmt.Errorf("SaveCurrentBlock error %s", err)
	}
	this.blockStore.SaveBlockHash(blockHeight, blockHash)
	err = this.blockStore.SaveBlock(block)
	if err != nil {
		return fmt.Errorf("SaveBlock height %d hash %s error %s", blockHeight, blockHash.ToHexString(), err)
	}
	return nil
}

func (this *LedgerStoreImp) executeBlock(block *types.Block) (result store.ExecuteResult, err error) {
	overlay := this.stateStore.NewOverlayDB()
	if block.Header.Height != 0 {
		config := &smartcontract.Config{
			ShardID:      block.Header.ShardID,
			Time:         block.Header.Timestamp,
			Height:       block.Header.Height,
			ParentHeight: block.Header.ParentHeight,
			Tx:           &types.Transaction{},
		}

		err = refreshGlobalParam(config, storage.NewCacheDB(this.stateStore.NewOverlayDB()), this)
		if err != nil {
			return
		}
	}
	gasTable := make(map[string]uint64)
	neovm.GAS_TABLE.Range(func(k, value interface{}) bool {
		key := k.(string)
		val := value.(uint64)
		gasTable[key] = val

		return true
	})

	addrList, err := this.stateStore.GetCurrentLockedAddress()
	if err != nil {
		return result, err
	}

	keyList, err := this.stateStore.GetCurrentLockedKeys()
	if err != nil {
		return result, err
	}

	lockedAddress := make(map[common.Address]struct{}, len(addrList))
	for _, addr := range addrList {
		lockedAddress[addr] = struct{}{}
	}
	lockedKeys := make(map[string]struct{})
	for _, key := range keyList {
		lockedKeys[string(key)] = struct{}{}
	}

	cache := storage.NewCacheDB(overlay)
	xshardDB := storage.NewXShardDB(overlay)
	var shardNotify []xshard_types.CommonShardMsg

	// execute shard txs
	// sort shard Txs
	ids := make([]common.ShardID, 0)
	for shardId := range block.ShardTxs {
		ids = append(ids, shardId)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].ToUint64() < ids[j].ToUint64() })
	for _, id := range ids {
		log.Infof("block shard %v: height %d executing shard Tx shard %v: txnum %d", block.Header.ShardID,
			block.Header.Height, id, len(block.ShardTxs[id]))
		for _, shardTx := range block.ShardTxs[id] {
			cache.Reset()
			if shardTx.Tx.TxType != types.ShardCall {
				txHash := shardTx.Tx.Hash()
				err = fmt.Errorf("handleTransaction failed tx type:%d,txHash:%s", types.ShardCall, txHash.ToHexString())
				return
			}
			notify, e := HandleTransaction(this, overlay, cache, gasTable, lockedAddress, lockedKeys, xshardDB,
				block.Header, shardTx.Tx)
			if e != nil {
				err = e
				return
			}
			shardNotify = append(shardNotify, notify.ShardMsg...)
			result.Notify = append(result.Notify, notify.ContractEvent)
		}
	}

	// execute transactions
	for _, tx := range block.Transactions {
		cache.Reset()
		if tx.TxType == types.ShardCall {
			txHash := tx.Hash()
			err = fmt.Errorf("handleTransaction failed tx type:%d,txHash:%s", types.ShardCall, txHash.ToHexString())
			return
		}
		notify, e := HandleTransaction(this, overlay, cache, gasTable, lockedAddress, lockedKeys, xshardDB,
			block.Header, tx)
		if e != nil {
			err = e
			return
		}

		shardNotify = append(shardNotify, notify.ShardMsg...)
		result.Notify = append(result.Notify, notify.ContractEvent)
	}

	xshardDB.SetXShardMsgInBlock(block.Header.Height, shardNotify)
	addrList = addrList[:0]
	for addr := range lockedAddress {
		addrList = append(addrList, addr)
	}
	keyList = keyList[:0]
	for key := range lockedKeys {
		keyList = append(keyList, []byte(key))
	}
	xshardDB.SetLockedAddress(addrList)
	xshardDB.SetLockedKeys(keyList)
	xshardDB.Commit()

	result.Hash = overlay.ChangeHash()
	result.WriteSet = overlay.GetWriteSet()
	result.ShardNotify = shardNotify
	if block.Header.Height < this.stateHashCheckHeight {
		result.MerkleRoot = common.UINT256_EMPTY
	} else if block.Header.Height == this.stateHashCheckHeight {
		res, e := calculateTotalStateHash(overlay)
		if e != nil {
			err = e
			return
		}

		result.MerkleRoot = res
		result.Hash = result.MerkleRoot
	} else {
		result.MerkleRoot = this.stateStore.GetStateMerkleRootWithNewHash(result.Hash)
	}

	return
}

func calculateTotalStateHash(overlay *overlaydb.OverlayDB) (result common.Uint256, err error) {
	stateDiff := sha256.New()
	iter := overlay.NewIterator([]byte{byte(scom.ST_CONTRACT)})
	err = accumulateHash(stateDiff, iter)
	iter.Release()
	if err != nil {
		return
	}

	iter = overlay.NewIterator([]byte{byte(scom.ST_STORAGE)})
	err = accumulateHash(stateDiff, iter)
	iter.Release()
	if err != nil {
		return
	}

	stateDiff.Sum(result[:0])
	return
}

func accumulateHash(hasher hash.Hash, iter scom.StoreIterator) error {
	for has := iter.First(); has; has = iter.Next() {
		key := iter.Key()
		val := iter.Value()
		hasher.Write(key)
		hasher.Write(val)
	}
	return iter.Error()
}

func (this *LedgerStoreImp) saveShardState(block *types.Block, result store.ExecuteResult) {
	shardSysMsg, _, _ := extractShardEvents(result.Notify)
	this.stateStore.AddBlockShardEvents(block.Header.Height, shardSysMsg)
}

func (this *LedgerStoreImp) saveBlockToStateStore(block *types.Block, result store.ExecuteResult) error {
	blockHash := block.Hash()
	blockHeight := block.Header.Height

	err := this.stateStore.AddStateMerkleTreeRoot(blockHeight, result.Hash)
	if err != nil {
		return fmt.Errorf("AddBlockMerkleTreeRoot error %s", err)
	}

	err = this.stateStore.AddBlockMerkleTreeRoot(block.Header.TransactionsRoot)
	if err != nil {
		return fmt.Errorf("AddBlockMerkleTreeRoot error %s", err)
	}

	err = this.stateStore.SaveCurrentBlock(blockHeight, blockHash)
	if err != nil {
		return fmt.Errorf("SaveCurrentBlock error %s", err)
	}

	this.saveShardState(block, result)

	log.Debugf("the state transition hash of block %d is:%s", blockHeight, result.Hash.ToHexString())

	result.WriteSet.ForEach(func(key, val []byte) {
		if len(val) == 0 {
			this.stateStore.BatchDeleteRawKey(key)
		} else {
			this.stateStore.BatchPutRawKeyVal(key, val)
		}
	})

	if config.DefConfig.Common.EnableEventLog {
		for _, notify := range result.Notify {
			if err := this.eventStore.SaveEventNotifyByTx(notify.TxHash, notify); err != nil {
				return fmt.Errorf("SaveEventNotifyByTx error %s", err)
			}
			event.PushSmartCodeEvent(notify.TxHash, 0, event.EVENT_NOTIFY, notify)

		}
	}

	return nil
}

func (this *LedgerStoreImp) saveBlockToEventStore(block *types.Block) error {
	blockHash := block.Hash()
	blockHeight := block.Header.Height
	txs := make([]common.Uint256, 0)
	for _, tx := range block.Transactions {
		txHash := tx.Hash()
		txs = append(txs, txHash)
	}
	if len(txs) > 0 {
		err := this.eventStore.SaveEventNotifyByBlock(block.Header.Height, txs)
		if err != nil {
			return fmt.Errorf("SaveEventNotifyByBlock error %s", err)
		}
	}
	err := this.eventStore.SaveCurrentBlock(blockHeight, blockHash)
	if err != nil {
		return fmt.Errorf("SaveCurrentBlock error %s", err)
	}
	return nil
}

func (this *LedgerStoreImp) saveCrossShardDataToStore(block *types.Block, result store.ExecuteResult) error {
	shardSysMsg, metaEvents, deployContractEvent := extractShardEvents(result.Notify)
	err := this.saveCrossShardGovernanceData(block, shardSysMsg, result.WriteSet)
	if err != nil {
		return err
	}
	err = this.saveParentShardConfig(block)
	if err != nil {
		return err
	}
	err = this.saveContractMetaData(metaEvents)
	if err != nil {
		return err
	}
	err = this.saveCrossShardDeployContractEventData(deployContractEvent)
	if err != nil {
		return err
	}
	return nil
}

func (this *LedgerStoreImp) saveCrossShardGovernanceData(block *types.Block, shardEvts []*message.ShardSystemEventMsg, overlayDB *overlaydb.MemDB) error {
	for _, evt := range shardEvts {
		shardEvt := evt.Event
		switch shardEvt.EventType {
		case shardstates.EVENT_SHARD_CONFIG_UPDATE:
			cfgEvt := &shardstates.ConfigShardEvent{}
			if err := cfgEvt.Deserialization(common.NewZeroCopySource(shardEvt.Payload)); err != nil {
				log.Errorf("deserialize update shard config event: %s", err)
				continue
			}
			return this.addShardEventConfig(cfgEvt.Height, cfgEvt.ImplSourceTargetShardID.ShardID, cfgEvt.Config, cfgEvt.Peers)
		case shardstates.EVENT_SHARD_ACTIVATED:
			evt := &shardstates.ShardActiveEvent{}
			if err := evt.Deserialization(common.NewZeroCopySource(shardEvt.Payload)); err != nil {
				log.Errorf("deserialize shard activation event: %s", err)
				continue
			}
			shardState := &shardstates.ShardState{}
			shardIDBytes := utils.GetUint64Bytes(evt.ShardID.ToUint64())
			key := append([]byte(shardmgmt.KEY_SHARD_STATE), shardIDBytes...)
			data, unknown := vconfig.GetRawStorageItemFromMemDb(overlayDB, utils.ShardMgmtContractAddress, key)
			if unknown {
				key := append([]byte(shardmgmt.KEY_SHARD_STATE), shardIDBytes...)
				storageKey := &states.StorageKey{
					ContractAddress: utils.ShardMgmtContractAddress,
					Key:             key,
				}
				storageItem, err := this.GetStorageItem(storageKey)
				if err != nil || storageItem == nil {
					log.Errorf("GetStorageItem err:%s", err)
					continue
				}
				if err := shardState.Deserialization(common.NewZeroCopySource(storageItem.Value)); err != nil {
					log.Errorf("deserialization storageitem value shard state: %s", err)
					continue
				}
			} else {
				value, err := states.GetValueFromRawStorageItem(data)
				if err != nil {
					log.Errorf("get shard state, deserialize from raw storage: %s", err)
					continue
				}
				if err := shardState.Deserialization(common.NewZeroCopySource(value)); err != nil {
					log.Errorf("deserialization storageitem value shrd state: %s", err)
					continue
				}
			}
			return this.addShardEventConfig(0, evt.ShardID, shardState.Config, shardState.Peers)
		}
	}
	return nil
}

func (this *LedgerStoreImp) saveParentShardConfig(block *types.Block) error {
	if config.DefConfig.Genesis.ConsensusType == config.CONSENSUS_TYPE_SOLO {
		return nil
	}
	blkInfo := &vconfig.VbftBlockInfo{}
	if err := json.Unmarshal(block.Header.ConsensusPayload, blkInfo); err != nil {
		return fmt.Errorf("unmarshal blockInfo: %s", err)
	}
	if blkInfo.LastConfigBlockNum != block.Header.Height {
		return nil
	}
	config := &shardstates.ShardConfig{
		VbftCfg: &config.VBFTConfig{
			N: blkInfo.NewChainConfig.N,
			C: blkInfo.NewChainConfig.C,
		},
	}
	peers := make(map[string]*shardstates.PeerShardStakeInfo)
	for _, peer := range blkInfo.NewChainConfig.Peers {
		peers[peer.ID] = &shardstates.PeerShardStakeInfo{
			Index:      peer.Index,
			PeerPubKey: peer.ID,
			NodeType:   shardstates.CONSENSUS_NODE,
		}
	}
	return this.addShardEventConfig(block.Header.Height, block.Header.ShardID, config, peers)
}

func (this *LedgerStoreImp) addShardEventConfig(height uint32, shardID common.ShardID, cfg *shardstates.ShardConfig, peers map[string]*shardstates.PeerShardStakeInfo) error {
	shardEvent := &shardstates.ConfigShardEvent{
		Height: height,
		Config: cfg,
		Peers:  peers,
	}
	sink := common.ZeroCopySink{}
	shardEvent.Serialization(&sink)
	err := this.eventStore.AddShardConsensusConfig(shardID, height, sink.Bytes())
	if err != nil {
		return fmt.Errorf("AddShardConsensusConfig err:%s", err)
	}

	heights, err := this.eventStore.GetShardConsensusHeight(shardID)
	if err != nil && err != scom.ErrNotFound {
		return fmt.Errorf("GetShardConsensusHeight shardID:%v, err:%s", shardID, err)
	}
	heights = append(heights, height)
	this.eventStore.AddShardConsensusHeight(shardID, heights)
	return nil
}

func (this *LedgerStoreImp) saveContractMetaData(metaEvents []*message.MetaDataEvent) error {
	for _, metaEvent := range metaEvents {
		if err := this.eventStore.SaveContractMetaDataEvent(metaEvent.Height, metaEvent.MetaData); err != nil {
			return err
		}
	}
	return nil
}
func (this *LedgerStoreImp) saveCrossShardDeployContractEventData(contractEvents []*message.ContractLifetimeEvent) error {
	for _, contractEvent := range contractEvents {
		err := this.eventStore.SaveContractEvent(contractEvent)
		if err != nil {
			return err
		}
	}
	return nil
}

func (this *LedgerStoreImp) tryGetSavingBlockLock() (hasLocked bool) {
	select {
	case this.savingBlockSemaphore <- true:
		return false
	default:
		return true
	}
}

func (this *LedgerStoreImp) getSavingBlockLock() {
	this.savingBlockSemaphore <- true
}

func (this *LedgerStoreImp) releaseSavingBlockLock() {
	select {
	case <-this.savingBlockSemaphore:
		return
	default:
		panic("can not release in unlocked state")
	}
}

//saveBlock do the job of execution samrt contract and commit block to store.
func (this *LedgerStoreImp) submitBlock(block *types.Block, result store.ExecuteResult) error {
	blockHash := block.Hash()
	blockHeight := block.Header.Height
	blockRoot := this.GetBlockRootWithNewTxRoots(block.Header.Height, []common.Uint256{block.Header.TransactionsRoot})
	if block.Header.Height != 0 && blockRoot != block.Header.BlockRoot {
		return fmt.Errorf("wrong block root at height:%d, expected:%s, got:%s",
			block.Header.Height, blockRoot.ToHexString(), block.Header.BlockRoot.ToHexString())
	}

	this.blockStore.NewBatch()
	this.stateStore.NewBatch()
	this.eventStore.NewBatch()
	err := this.saveBlockToBlockStore(block)
	if err != nil {
		return fmt.Errorf("save to block store height:%d error:%s", blockHeight, err)
	}
	err = this.saveBlockToStateStore(block, result)
	if err != nil {
		return fmt.Errorf("save to state store height:%d error:%s", blockHeight, err)
	}
	err = this.saveBlockToEventStore(block)
	if err != nil {
		return fmt.Errorf("save to event store height:%d error:%s", blockHeight, err)
	}

	err = this.saveCrossShardDataToStore(block, result)
	if err != nil {
		return fmt.Errorf("save to save cross shard data height:%d error:%s", blockHeight, err)
	}
	err = this.blockStore.CommitTo()
	if err != nil {
		return fmt.Errorf("blockStore.CommitTo height:%d error %s", blockHeight, err)
	}
	// event store is idempotent to re-save when in recovering process, so save first before stateStore
	err = this.eventStore.CommitTo()
	if err != nil {
		return fmt.Errorf("eventStore.CommitTo height:%d error %s", blockHeight, err)
	}
	err = this.stateStore.CommitTo()
	if err != nil {
		return fmt.Errorf("stateStore.CommitTo height:%d error %s", blockHeight, err)
	}
	this.setCurrentBlock(blockHeight, blockHash)

	shardSysMsg, _, _ := extractShardEvents(result.Notify)
	sourceAndShardTxHashMap := extractSourceAndShardTxHash(result.Notify)
	if events.DefActorPublisher != nil {
		events.DefActorPublisher.Publish(
			message.TOPIC_SAVE_BLOCK_COMPLETE,
			&message.SaveBlockCompleteMsg{
				Block:                   block,
				ShardSysEvents:          shardSysMsg,
				SourceAndShardTxHashMap: sourceAndShardTxHashMap,
			})
	}
	return nil
}

func extractSourceAndShardTxHash(notify []*event.ExecuteNotify) map[common.Uint256]common.Uint256 {
	sourceAndShardTxHash := make(map[common.Uint256]common.Uint256)
	for _, n := range notify {
		if n.SourceTxHash == nil || len(n.SourceTxHash) == 0 {
			continue
		}
		for _, sourceTxHash := range n.SourceTxHash {
			sourceAndShardTxHash[sourceTxHash] = n.TxHash
		}
	}
	return sourceAndShardTxHash
}
func extractShardEvents(notify []*event.ExecuteNotify) ([]*message.ShardSystemEventMsg, []*message.MetaDataEvent,
	[]*message.ContractLifetimeEvent) {
	var shardSysMsg []*message.ShardSystemEventMsg
	metaEvents := make([]*message.MetaDataEvent, 0)
	contractEvents := make([]*message.ContractLifetimeEvent, 0)
	for _, txEvents := range notify {
		for _, n := range txEvents.Notify {
			if n.ContractAddress == utils.ShardMgmtContractAddress ||
				n.ContractAddress == utils.ShardSysMsgContractAddress {
				if shardEvt, ok := n.States.(*message.ShardEventState); ok {
					shardSysMsg = append(shardSysMsg, &message.ShardSystemEventMsg{
						FromAddress: n.ContractAddress,
						Event:       shardEvt,
					})
				}
			} else if evt, ok := n.States.(*message.MetaDataEvent); ok {
				metaEvents = append(metaEvents, evt)
			} else if evt, ok := n.States.(*message.ContractLifetimeEvent); ok {
				contractEvents = append(contractEvents, evt)
			}
		}
	}

	return shardSysMsg, metaEvents, contractEvents
}

func HandleTransaction(store store.LedgerStore, overlay *overlaydb.OverlayDB, cache *storage.CacheDB, gasTable map[string]uint64,
	lockedAddress map[common.Address]struct{}, lockedKeys map[string]struct{}, xshardDB *storage.XShardDB, header *types.Header, tx *types.Transaction) (*event.TransactionNotify, error) {
	txHash := tx.Hash()
	events := &event.ExecuteNotify{TxHash: txHash, State: event.CONTRACT_STATE_FAIL}
	notify := &event.TransactionNotify{
		ContractEvent: events,
	}
	switch tx.TxType {
	case types.Deploy:
		err := HandleDeployTransaction(store, overlay, gasTable, cache, tx, header, notify.ContractEvent)
		if overlay.Error() != nil {
			return nil, fmt.Errorf("HandleDeployTransaction tx %s error %s", txHash.ToHexString(), overlay.Error())
		}
		cache.Commit()
		if err != nil {
			log.Debugf("HandleDeployTransaction tx %s error %s", txHash.ToHexString(), err)
		}
	case types.MetaData:
		err := HandleChangeMetadataTransaction(store, overlay, gasTable, cache, tx, header, notify.ContractEvent)
		if overlay.Error() != nil {
			return nil, fmt.Errorf("HandleChangeMetadataTransaction tx %s error %s", txHash.ToHexString(), overlay.Error())
		}
		if err != nil {
			log.Debugf("HandleChangeMetadataTransaction tx %s error %s", txHash.ToHexString(), err)
		}
	case types.Invoke:
		_, err := HandleInvokeTransaction(store, overlay, gasTable, lockedAddress, lockedKeys, cache, xshardDB, tx,
			header, notify)
		if overlay.Error() != nil {
			return nil, fmt.Errorf("HandleInvokeTransaction tx %s error %s", txHash.ToHexString(), overlay.Error())
		}
		cache.Commit()
		if err != nil {
			log.Debugf("HandleInvokeTransaction tx %s error %s", txHash.ToHexString(), err)
		}
	case types.ShardCall:
		shardCall := tx.Payload.(*payload.ShardCall)
		err := HandleShardCallTransaction(store, overlay, gasTable, lockedAddress, lockedKeys, cache, xshardDB,
			shardCall.Msgs, header, notify)
		if overlay.Error() != nil {
			return nil, fmt.Errorf("HandleDeployTransaction tx %s error %s", txHash.ToHexString(), overlay.Error())
		}
		cache.Commit()
		if err != nil {
			log.Debugf("HandleShardCallTransaction tx %s error %s", txHash.ToHexString(), err)
		}
	}

	return notify, nil
}

func (this *LedgerStoreImp) saveHeaderIndexList() error {
	this.lock.RLock()
	storeCount := this.storedIndexCount
	currHeight := this.currBlockHeight
	if currHeight-storeCount < HEADER_INDEX_BATCH_SIZE {
		this.lock.RUnlock()
		return nil
	}

	headerList := make([]common.Uint256, HEADER_INDEX_BATCH_SIZE)
	for i := uint32(0); i < HEADER_INDEX_BATCH_SIZE; i++ {
		height := storeCount + i
		headerList[i] = this.headerIndex[height]
	}
	this.lock.RUnlock()

	err := this.blockStore.SaveHeaderIndexList(storeCount, headerList)
	if err != nil {
		return fmt.Errorf("SaveHeaderIndexList start %d error %s", storeCount, err)
	}

	this.lock.Lock()
	this.storedIndexCount += HEADER_INDEX_BATCH_SIZE
	this.lock.Unlock()
	return nil
}

//IsContainBlock return whether the block is in store
func (this *LedgerStoreImp) IsContainBlock(blockHash common.Uint256) (bool, error) {
	return this.blockStore.ContainBlock(blockHash)
}

//IsContainTransaction return whether the transaction is in store. Wrap function of BlockStore.ContainTransaction
func (this *LedgerStoreImp) IsContainTransaction(txHash common.Uint256) (bool, error) {
	return this.blockStore.ContainTransaction(txHash)
}

//IsContainShardTx return whether the ShardTx is in store. Wrap function of BlockStore.ContainShardTx
func (this *LedgerStoreImp) IsContainShardTx(shardTxHash common.Uint256) (bool, error) {
	return this.blockStore.ContainShardTx(shardTxHash)
}

//GetBlockRootWithNewTxRoots return the block root(merkle root of blocks) after add a new tx root of block
func (this *LedgerStoreImp) GetBlockRootWithNewTxRoots(startHeight uint32, txRoots []common.Uint256) common.Uint256 {
	this.lock.RLock()
	defer this.lock.RUnlock()
	// the block height in consensus is far behind ledger, this case should be rare
	if this.currBlockHeight > startHeight+uint32(len(txRoots))-1 {
		// or return error?
		return common.UINT256_EMPTY
	}

	needs := txRoots[this.currBlockHeight+1-startHeight:]
	return this.stateStore.GetBlockRootWithNewTxRoots(needs)
}

//GetBlockHash return the block hash by block height
func (this *LedgerStoreImp) GetBlockHash(height uint32) common.Uint256 {
	return this.getHeaderIndex(height)
}

//GetHeaderByHash return the block header by block hash
func (this *LedgerStoreImp) GetHeaderByHash(blockHash common.Uint256) (*types.Header, error) {
	header := this.getHeaderCache(blockHash)
	if header != nil {
		return header, nil
	}
	return this.blockStore.GetHeader(blockHash)
}

func (this *LedgerStoreImp) GetRawHeaderByHash(blockHash common.Uint256) (*types.RawHeader, error) {
	header := this.getHeaderCache(blockHash)
	if header != nil {
		return header.GetRawHeader(), nil
	}
	return this.blockStore.GetRawHeader(blockHash)
}

//GetHeaderByHash return the block header by block height
func (this *LedgerStoreImp) GetHeaderByHeight(height uint32) (*types.Header, error) {
	blockHash := this.GetBlockHash(height)
	var empty common.Uint256
	if blockHash == empty {
		return nil, nil
	}
	return this.GetHeaderByHash(blockHash)
}

//GetSysFeeAmount return the sys fee for block by block hash. Wrap function of BlockStore.GetSysFeeAmount
func (this *LedgerStoreImp) GetSysFeeAmount(blockHash common.Uint256) (common.Fixed64, error) {
	return this.blockStore.GetSysFeeAmount(blockHash)
}

//GetTransaction return transaction by transaction hash. Wrap function of BlockStore.GetTransaction
func (this *LedgerStoreImp) GetTransaction(txHash common.Uint256) (*types.Transaction, uint32, error) {
	return this.blockStore.GetTransaction(txHash)
}

//GetBlockByHash return block by block hash. Wrap function of BlockStore.GetBlockByHash
func (this *LedgerStoreImp) GetBlockByHash(blockHash common.Uint256) (*types.Block, error) {
	return this.blockStore.GetBlock(blockHash)
}

//GetBlockByHeight return block by height.
func (this *LedgerStoreImp) GetBlockByHeight(height uint32) (*types.Block, error) {
	blockHash := this.GetBlockHash(height)
	var empty common.Uint256
	if blockHash == empty {
		return nil, nil
	}
	return this.GetBlockByHash(blockHash)
}

//GetBookkeeperState return the bookkeeper state. Wrap function of StateStore.GetBookkeeperState
func (this *LedgerStoreImp) GetBookkeeperState() (*states.BookkeeperState, error) {
	return this.stateStore.GetBookkeeperState()
}

//GetMerkleProof return the block merkle proof. Wrap function of StateStore.GetMerkleProof
func (this *LedgerStoreImp) GetMerkleProof(proofHeight, rootHeight uint32) ([]common.Uint256, error) {
	return this.stateStore.GetMerkleProof(proofHeight, rootHeight)
}

//GetContractState return contract by contract address. Wrap function of StateStore.GetContractState
func (this *LedgerStoreImp) GetContractState(contractHash common.Address) (*payload.DeployCode, error) {
	return this.stateStore.GetContractState(contractHash)
}

//GetContractState return contract by contract address. Wrap function of StateStore.GetContractState
func (this *LedgerStoreImp) GetContractMetaData(contractHash common.Address) (*payload.MetaDataCode, error) {
	return this.stateStore.GetContractMetaData(contractHash)
}

//GetStorageItem return the storage value of the key in smart contract. Wrap function of StateStore.GetStorageState
func (this *LedgerStoreImp) GetStorageItem(key *states.StorageKey) (*states.StorageItem, error) {
	return this.stateStore.GetStorageState(key)
}

//GetEventNotifyByTx return the events notify gen by executing of smart contract.  Wrap function of EventStore.GetEventNotifyByTx
func (this *LedgerStoreImp) GetEventNotifyByTx(tx common.Uint256) (*event.ExecuteNotify, error) {
	return this.eventStore.GetEventNotifyByTx(tx)
}

//GetEventNotifyByBlock return the transaction hash which have event notice after execution of smart contract. Wrap function of EventStore.GetEventNotifyByBlock
func (this *LedgerStoreImp) GetEventNotifyByBlock(height uint32) ([]*event.ExecuteNotify, error) {
	return this.eventStore.GetEventNotifyByBlock(height)
}

//PreExecuteContract return the result of smart contract execution without commit to store
func (this *LedgerStoreImp) PreExecuteContract(tx *types.Transaction) (*sstate.PreExecResult, error) {
	height := this.GetCurrentBlockHeight()
	stf := &sstate.PreExecResult{State: event.CONTRACT_STATE_FAIL, Gas: neovm.MIN_TRANSACTION_GAS, Result: nil}
	header, err := this.GetHeaderByHeight(height)
	if err != nil {
		return stf, err
	}

	config := &smartcontract.Config{
		ShardID:      tx.ShardID,
		Time:         uint32(time.Now().Unix()),
		Height:       height + 1,
		ParentHeight: header.ParentHeight,
		Tx:           tx,
		BlockHash:    this.GetBlockHash(height),
	}

	overlay := this.stateStore.NewOverlayDB()
	cache := storage.NewCacheDB(overlay)
	preGas, err := this.getPreGas(config, cache)
	if err != nil {
		return stf, err
	}

	if tx.TxType == types.Invoke {
		invoke := tx.Payload.(*payload.InvokeCode)

		sc := smartcontract.SmartContract{
			Config:  config,
			Store:   this,
			CacheDB: cache,
			Gas:     math.MaxUint64 - calcGasByCodeLen(len(invoke.Code), preGas[neovm.UINT_INVOKE_CODE_LEN_NAME]),
			PreExec: true,
		}

		//start the smart contract executive function
		engine, _ := sc.NewExecuteEngine(invoke.Code)
		result, err := engine.Invoke()
		if err != nil {
			return stf, err
		}
		gasCost := math.MaxUint64 - sc.Gas
		mixGas := neovm.MIN_TRANSACTION_GAS
		if gasCost < mixGas {
			gasCost = mixGas
		}
		cv, err := scommon.ConvertNeoVmTypeHexString(result)
		if err != nil {
			return stf, err
		}
		return &sstate.PreExecResult{State: event.CONTRACT_STATE_SUCCESS, Gas: gasCost, Result: cv, Notify: sc.Notifications}, nil
	} else if tx.TxType == types.Deploy {
		deploy := tx.Payload.(*payload.DeployCode)
		return &sstate.PreExecResult{State: event.CONTRACT_STATE_SUCCESS, Gas: preGas[neovm.CONTRACT_CREATE_NAME] + calcGasByCodeLen(len(deploy.Code), preGas[neovm.UINT_DEPLOY_CODE_LEN_NAME]), Result: nil}, nil
	} else if tx.TxType == types.ShardCall || tx.TxType == types.MetaData {
		return stf, nil
	} else {
		return stf, errors.NewErr("transaction type error")
	}
}

func (this *LedgerStoreImp) getPreGas(config *smartcontract.Config, cache *storage.CacheDB) (map[string]uint64, error) {
	bf := new(bytes.Buffer)
	names := []string{neovm.CONTRACT_CREATE_NAME, neovm.UINT_INVOKE_CODE_LEN_NAME, neovm.UINT_DEPLOY_CODE_LEN_NAME}
	if err := utils.WriteVarUint(bf, uint64(len(names))); err != nil {
		return nil, fmt.Errorf("write gas_table_keys length error:%s", err)
	}

	for _, v := range names {
		if err := serialization.WriteString(bf, v); err != nil {
			return nil, fmt.Errorf("serialize param name error:%s", err)
		}
	}

	sc := smartcontract.SmartContract{
		Config:  config,
		CacheDB: cache,
		Store:   this,
		Gas:     math.MaxUint64,
	}

	service, _ := sc.NewNativeService()
	result, err := service.NativeCall(utils.ParamContractAddress, "getGlobalParam", bf.Bytes())
	if err != nil {
		return nil, err
	}
	params := new(global_params.Params)
	if err := params.Deserialize(bytes.NewBuffer(result.([]byte))); err != nil {
		return nil, fmt.Errorf("deserialize global params error:%s", err)
	}
	m := make(map[string]uint64, 0)
	for _, v := range names {
		n, ps := params.GetParam(v)
		if n != -1 && ps.Value != "" {
			pu, err := strconv.ParseUint(ps.Value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse uint %v", err)
			}
			m[v] = pu
		}
	}
	return m, nil
}

func (self *LedgerStoreImp) GetBlockShardEvents(height uint32) (events []*message.ShardSystemEventMsg, err error) {
	return self.stateStore.GetBlockShardEvents(height)
}

func (self *LedgerStoreImp) GetShardMsgsInBlock(blockHeight uint32, shardID common.ShardID) ([]xshard_types.CommonShardMsg, error) {
	return self.stateStore.GetShardMsgsInBlock(blockHeight, shardID)
}

func (self *LedgerStoreImp) GetRelatedShardIDsInBlock(blockHeight uint32) ([]common.ShardID, error) {
	return self.stateStore.GetRelatedShardIDsInBlock(blockHeight)
}

func (self *LedgerStoreImp) GetParentMetaData(blockHeight uint32, contractAddr common.Address) (*payload.MetaDataCode, error) {
	if self.parentShardStore == nil {
		return nil, fmt.Errorf("parent ledger nil")
	}
	return self.parentShardStore.GetMetaDataEvnet(blockHeight, contractAddr)
}

func (self *LedgerStoreImp) GetParentContract(blockHeight uint32, addr common.Address) (*payload.DeployCode, error) {
	if self.parentShardStore == nil {
		return nil, fmt.Errorf("parent ledger nil")
	}
	evt, err := self.parentShardStore.GetContractEvent(addr)
	if err != nil {
		return nil, err
	}
	if evt.Destroyed && evt.DestroyHeight < blockHeight {
		return nil, fmt.Errorf("contract has already destoryed")
	}
	return evt.Contract, nil
}

func (self *LedgerStoreImp) GetShardConsensusHeight(shardID common.ShardID) ([]uint32, error) {
	return self.eventStore.GetShardConsensusHeight(shardID)
}

func (self *LedgerStoreImp) GetShardConsensusConfig(shardID common.ShardID, height uint32) ([]byte, error) {
	return self.eventStore.GetShardConsensusConfig(shardID, height)
}

//Close ledger store.
func (this *LedgerStoreImp) Close() error {
	err := this.blockStore.Close()
	if err != nil {
		return fmt.Errorf("blockStore close error %s", err)
	}
	err = this.stateStore.Close()
	if err != nil {
		return fmt.Errorf("stateStore close error %s", err)
	}
	err = this.eventStore.Close()
	if err != nil {
		return fmt.Errorf("eventStore close error %s", err)
	}
	return nil
}

func (this *LedgerStoreImp) GetContractEvent(addr common.Address) (*message.ContractLifetimeEvent, error) {
	return this.eventStore.GetContractEvent(addr)
}

func (this *LedgerStoreImp) GetMetaDataEvnet(height uint32, addr common.Address) (*payload.MetaDataCode, error) {
	return this.eventStore.GetContractMetaDataEvent(height, addr)
}

func (this *LedgerStoreImp) GetShardTxState(txHash common.Uint256, notifyId uint32, hasNotifyId bool) (*xshard_state.TxState, error) {
	overlay := this.stateStore.NewOverlayDB()
	xshardDB := storage.NewXShardDB(overlay)

	var shardTxID xshard_types.ShardTxID
	shardTxID = xshard_types.ShardTxID(string(txHash[:]))
	if !hasNotifyId {
		return xshardDB.GetXShardState(shardTxID)
	}
	sink := common.NewZeroCopySink(0)
	sink.WriteBytes([]byte(shardTxID))
	sink.WriteUint32(notifyId)
	crossShardTxID := xshard_types.ShardTxID(string(sink.Bytes()))
	return xshardDB.GetXShardState(crossShardTxID)
}
