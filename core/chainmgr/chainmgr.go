/*
 * Copyright (C) 2019 The ontology Authors
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

package chainmgr

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/ontio/ontology-eventbus/actor"
	"github.com/ontio/ontology/account"
	"github.com/ontio/ontology/cmd/utils"
	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/config"
	"github.com/ontio/ontology/common/log"
	"github.com/ontio/ontology/consensus"
	"github.com/ontio/ontology/core/chainmgr/xshard"
	"github.com/ontio/ontology/core/genesis"
	"github.com/ontio/ontology/core/ledger"
	com "github.com/ontio/ontology/core/store/common"
	"github.com/ontio/ontology/core/types"
	"github.com/ontio/ontology/events"
	"github.com/ontio/ontology/events/message"
	actor2 "github.com/ontio/ontology/http/base/actor"
	"github.com/ontio/ontology/p2pserver/actor/req"
	"github.com/ontio/ontology/p2pserver/actor/server"
	p2pmsg "github.com/ontio/ontology/p2pserver/message/types"
	shardstates "github.com/ontio/ontology/smartcontract/service/native/shardmgmt/states"
	"github.com/ontio/ontology/txnpool"
	tc "github.com/ontio/ontology/txnpool/common"
	"github.com/ontio/ontology/validator/stateful"
	"github.com/ontio/ontology/validator/stateless"
)

const (
	CAP_LOCAL_SHARDMSG_CHNL = 64
	CAP_CROSS_SHARDMSG_CHNL = 64
	CAP_SHARD_BLOCK_POOL    = 16
)

var defaultChainManager *ChainManager = nil

//
// ShardInfo:
//  . Configuration of other shards
//  . seed list of other shards
//
type ShardInfo struct {
	ShardID  common.ShardID
	SeedList []string
	Config   *config.OntologyConfig
}

type ChainManager struct {
	shardID common.ShardID

	// ShardInfo management, indexing shards with ShardID / Sender-Addr
	lock      sync.RWMutex
	shards    map[common.ShardID]*ShardInfo
	consensus consensus.ConsensusService

	account *account.Account

	// send transaction to local
	p2pPid    *actor.PID
	localPid  *actor.PID
	txPoolMgr *txnpool.TxnPoolManager

	// subscribe local SHARD_EVENT from shard-system-contract and BLOCK-EVENT from ledger
	localEventSub  *events.ActorSubscriber
	localBlockMsgC chan *message.SaveBlockCompleteMsg
	crossShardMsgC chan *p2pmsg.CrossShardPayload

	quitC  chan struct{}
	quitWg sync.WaitGroup
}

//
// Initialize chain manager when ontology starting
//
func Initialize(shardID common.ShardID, acc *account.Account) (*ChainManager, error) {
	if defaultChainManager != nil {
		return nil, fmt.Errorf("chain manager had been initialized for shard: %d", defaultChainManager.shardID)
	}

	xshard.InitCrossShardPool(shardID, CAP_SHARD_BLOCK_POOL)

	chainMgr := &ChainManager{
		shardID:        shardID,
		shards:         make(map[common.ShardID]*ShardInfo),
		localBlockMsgC: make(chan *message.SaveBlockCompleteMsg, CAP_LOCAL_SHARDMSG_CHNL),
		crossShardMsgC: make(chan *p2pmsg.CrossShardPayload, CAP_CROSS_SHARDMSG_CHNL),
		quitC:          make(chan struct{}),

		account: acc,
	}
	go chainMgr.localEventLoop()
	go chainMgr.crossShardEventLoop()
	props := actor.FromProducer(func() actor.Actor {
		return chainMgr
	})
	pid, err := actor.SpawnNamed(props, GetShardName(shardID))
	if err == nil {
		chainMgr.localPid = pid
	}
	defaultChainManager = chainMgr
	return defaultChainManager, nil
}

//
// LoadFromLedger when ontology starting, after ledger initialized.
//
func (self *ChainManager) LoadFromLedger(stateHashHeight uint32) error {
	if err := self.initMainLedger(stateHashHeight); err != nil {
		return err
	}

	if self.shardID.ToUint64() == config.DEFAULT_SHARD_ID {
		// main-chain node, not need to process shard-events
		return nil
	}

	shardState, err := xshard.GetShardState(ledger.GetShardLedger(common.NewShardIDUnchecked(config.DEFAULT_SHARD_ID)), self.shardID)
	if err == com.ErrNotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get shard %d failed: %s", self.shardID, err)
	}
	// skip if shard is not active
	if shardState.State != shardstates.SHARD_STATE_ACTIVE {
		return nil
	}
	shardInfo := self.initShardInfo(shardState)
	cfg, err := self.buildShardConfig(self.shardID, shardState)
	if err != nil {
		return fmt.Errorf("init shard %d, failed to build config: %s", self.shardID, err)
	}
	shardInfo.Config = cfg

	if err := self.initShardLedger(shardInfo); err != nil {
		return fmt.Errorf("init shard %d, failed to init ledger: %s", self.shardID, err)
	}

	return nil
}

func (self *ChainManager) initMainLedger(stateHashHeight uint32) error {
	dbDir := utils.GetStoreDirPath(config.DefConfig.Common.DataDir, config.DefConfig.P2PNode.NetworkName)
	lgr, err := ledger.NewLedger(dbDir, stateHashHeight)
	if err != nil {
		return fmt.Errorf("NewLedger error:%s", err)
	}
	bookKeepers, err := config.DefConfig.GetBookkeepers()
	if err != nil {
		return fmt.Errorf("GetBookkeepers error:%s", err)
	}
	cfg := config.DefConfig
	genesisConfig := config.DefConfig.Genesis
	shardConfig := config.DefConfig.Shard
	genesisBlock, err := genesis.BuildGenesisBlock(bookKeepers, genesisConfig, shardConfig)
	if err != nil {
		return fmt.Errorf("genesisBlock error %s", err)
	}
	err = lgr.Init(bookKeepers, genesisBlock)
	if err != nil {
		return fmt.Errorf("Init ledger error:%s", err)
	}

	mainShardID := common.NewShardIDUnchecked(config.DEFAULT_SHARD_ID)
	mainShardInfo := &ShardInfo{
		ShardID:  mainShardID,
		SeedList: cfg.Genesis.SeedList,
		Config:   cfg,
	}
	self.shards[mainShardID] = mainShardInfo
	ledger.DefLedger = lgr
	log.Infof("main ledger init success")
	return nil
}

func (self *ChainManager) initShardLedger(shardInfo *ShardInfo) error {
	if ledger.GetShardLedger(shardInfo.ShardID) != nil {
		return nil
	}
	if self.shardID.ToUint64() == config.DEFAULT_SHARD_ID {
		return fmt.Errorf("init main ledger as shard ledger")
	}
	dbDir := utils.GetStoreDirPath(config.DefConfig.Common.DataDir, config.DefConfig.P2PNode.NetworkName)
	lgr, err := ledger.NewShardLedger(self.shardID, dbDir, ledger.GetShardLedger(common.NewShardIDUnchecked(config.DEFAULT_SHARD_ID)))
	if err != nil {
		return fmt.Errorf("init shard ledger: %s", err)
	}
	bookKeepers, err := shardInfo.Config.GetBookkeepers()
	if err != nil {
		return fmt.Errorf("init shard ledger: GetBookkeepers error:%s", err)
	}
	genesisConfig := shardInfo.Config.Genesis
	shardConfig := shardInfo.Config.Shard
	genesisBlock, err := genesis.BuildGenesisBlock(bookKeepers, genesisConfig, shardConfig)
	if err != nil {
		return fmt.Errorf("init shard ledger: genesisBlock error %s", err)
	}
	err = lgr.Init(bookKeepers, genesisBlock)
	if err != nil {
		return fmt.Errorf("init shard ledger: :%s", err)
	}
	err = xshard.InitShardInfo(lgr)
	if err != nil {
		return fmt.Errorf("init shard ledger: :%s", err)
	}
	return nil
}

func (self *ChainManager) GetActiveShards() []common.ShardID {
	shards := make([]common.ShardID, 0)
	for _, shardInfo := range self.shards {
		shards = append(shards, shardInfo.ShardID)
	}
	return shards
}

func (self *ChainManager) startConsensus() error {
	if self.consensus != nil || self.account == nil {
		return nil
	}

	// start consensus
	shardInfo := self.shards[self.shardID]
	if shardInfo == nil {
		return fmt.Errorf("shard %d starting consensus, shard info not available", self.shardID.ToUint64())
	}
	if shardInfo.Config == nil {
		return fmt.Errorf("shard %d starting consensus, shard config not available", self.shardID.ToUint64())
	}
	lgr := ledger.GetShardLedger(self.shardID)
	if lgr == nil {
		return fmt.Errorf("shard %d starting consensus, shard ledger not available", self.shardID.ToUint64())
	}

	// TODO: check if peer should start consensus
	if !shardInfo.Config.Consensus.EnableConsensus {
		return nil
	}

	txPoolPid := self.txPoolMgr.GetPID(self.shardID, tc.TxPoolActor)
	if txPoolPid == nil {
		return fmt.Errorf("shard %d staring consensus, shard txPool not availed", self.shardID.ToUint64())
	}

	consensusType := shardInfo.Config.Genesis.ConsensusType
	consensusService, err := consensus.NewConsensusService(consensusType, self.shardID, self.account, txPoolPid, lgr, self.p2pPid)
	if err != nil {
		return fmt.Errorf("NewConsensusService:%s error:%s", consensusType, err)
	}
	consensusService.Start()
	self.consensus = consensusService

	actor2.SetConsensusPid(consensusService.GetPID())
	req.SetConsensusPid(consensusService.GetPID())
	return nil
}

func (self *ChainManager) initShardTxPool() error {
	lgr := ledger.GetShardLedger(self.shardID)
	if lgr == nil {
		log.Infof("shard %d starting consensus, shard ledger not available", self.shardID.ToUint64())
		return nil
	}
	srv, err := self.txPoolMgr.StartTxnPoolServer(self.shardID, lgr)
	if err != nil {
		return fmt.Errorf("Init txpool error:%s", err)
	}
	stlValidator, _ := stateless.NewValidator(fmt.Sprintf("stateless_validator_%d", self.shardID.ToUint64()))
	stlValidator.Register(srv.GetPID(tc.VerifyRspActor))
	stlValidator2, _ := stateless.NewValidator(fmt.Sprintf("stateless_validator2_%d", self.shardID.ToUint64()))
	stlValidator2.Register(srv.GetPID(tc.VerifyRspActor))
	stfValidator, _ := stateful.NewValidator(fmt.Sprintf("stateful_validator_%d", self.shardID.ToUint64()), lgr)
	stfValidator.Register(srv.GetPID(tc.VerifyRspActor))
	return nil
}

func (self *ChainManager) Start(p2pPid *actor.PID, txPoolMgr *txnpool.TxnPoolManager) error {
	self.p2pPid = p2pPid
	self.txPoolMgr = txPoolMgr
	// start listen on local shard events
	self.localEventSub = events.NewActorSubscriber(self.localPid)
	req.SetChainMgrPid(self.localPid)
	self.localEventSub.Subscribe(message.TOPIC_SHARD_SYSTEM_EVENT)
	self.localEventSub.Subscribe(message.TOPIC_SAVE_BLOCK_COMPLETE)

	syncerToStart := make([]common.ShardID, 0)
	for shardId := self.shardID; shardId.ToUint64() != config.DEFAULT_SHARD_ID; shardId = shardId.ParentID() {
		syncerToStart = append(syncerToStart, shardId)
	}
	// start syncing root-chain
	syncerToStart = append(syncerToStart, common.NewShardIDUnchecked(config.DEFAULT_SHARD_ID))

	// start syncing shard
	for i := len(syncerToStart) - 1; i >= 0; i-- {
		shardId := syncerToStart[i]
		if self.shards[shardId] != nil {
			p2pPid.Tell(&server.StartSync{
				ShardID:    shardId.ToUint64(),
				ShardSeeds: self.shards[shardId].SeedList,
			})
			log.Infof("chainmgr starting shard-sync %d", shardId)
		}
	}

	return self.startConsensus()
}

func (self *ChainManager) Receive(context actor.Context) {
	switch msg := context.Message().(type) {
	case *actor.Restarting:
		log.Info("chain mgr actor restarting")
	case *actor.Stopping:
		log.Info("chain mgr actor stopping")
	case *actor.Stopped:
		log.Info("chain mgr actor stopped")
	case *actor.Started:
		log.Info("chain mgr actor started")
	case *actor.Restart:
		log.Info("chain mgr actor restart")
	case *message.SaveBlockCompleteMsg:
		self.localBlockMsgC <- msg
	case *p2pmsg.CrossShardPayload:
		self.crossShardMsgC <- msg
	default:
		log.Info("chain mgr actor: Unknown msg ", msg, "type", reflect.TypeOf(msg))
	}
}

// handle shard system contract event, other events are not handled and returned
func (self *ChainManager) handleShardSysEvents(shardEvts []*message.ShardSystemEventMsg) {
	for _, evt := range shardEvts {
		shardEvt := evt.Event
		switch shardEvt.EventType {
		case shardstates.EVENT_SHARD_CREATE:
			createEvt := &shardstates.CreateShardEvent{}
			if err := createEvt.Deserialization(common.NewZeroCopySource(shardEvt.Payload)); err != nil {
				log.Errorf("deserialize create shard event: %s", err)
				continue
			}
			if err := self.onShardCreated(createEvt); err != nil {
				log.Errorf("processing create shard event: %s", err)
			}
		case shardstates.EVENT_SHARD_CONFIG_UPDATE:
			cfgEvt := &shardstates.ConfigShardEvent{}
			if err := cfgEvt.Deserialization(common.NewZeroCopySource(shardEvt.Payload)); err != nil {
				log.Errorf("deserialize update shard config event: %s", err)
				continue
			}
			if err := self.onShardConfigured(cfgEvt); err != nil {
				log.Errorf("processing update shard config event: %s", err)
			}
		case shardstates.EVENT_SHARD_PEER_JOIN:
			jointEvt := &shardstates.PeerJoinShardEvent{}
			if err := jointEvt.Deserialization(common.NewZeroCopySource(shardEvt.Payload)); err != nil {
				log.Errorf("deserialize join shard event: %s", err)
				continue
			}
			if err := self.onShardPeerJoint(jointEvt); err != nil {
				log.Errorf("processing join shard event: %s", err)
			}
		case shardstates.EVENT_SHARD_ACTIVATED:
			evt := &shardstates.ShardActiveEvent{}
			if err := evt.Deserialization(common.NewZeroCopySource(shardEvt.Payload)); err != nil {
				log.Errorf("deserialize shard activation event: %s", err)
				continue
			}
			if err := self.onShardActivated(evt); err != nil {
				log.Errorf("processing shard activation event: %s", err)
			}
		case shardstates.EVENT_SHARD_PEER_LEAVE:
		}
	}
}

func (self *ChainManager) handleCrossShardMsg(payload *p2pmsg.CrossShardPayload) {
	if payload.ShardID != self.shardID {
		return
	}
	source := common.NewZeroCopySource(payload.Data)
	msg := &types.CrossShardMsg{}
	if err := msg.Deserialization(source); err != nil {
		log.Errorf("handleCrossShardMsg failed to Deserialize crossshard msg %s", err)
		return
	}
	err := xshard.AddCrossShardInfo(ledger.GetShardLedger(self.shardID), msg)
	if err != nil {
		log.Errorf("handleCrossShardMsg AddCrossShardInfo err:%s", err)
	}
}

//
// localEventLoop: process all local shard-event.
//   shard-events are from shard system contracts (shard-mgmt, shard-gas, shard-mq, shard-ccmc)
//
func (self *ChainManager) localEventLoop() {
	self.quitWg.Add(1)
	defer self.quitWg.Done()

	for {
		select {
		case msg := <-self.localBlockMsgC:
			self.handleShardSysEvents(msg.ShardSysEvents)
			blk := msg.Block
			self.onBlockPersistCompleted(blk)
			if msg.SourceAndShardTxHashMap != nil {
				self.saveSourceAndShardTxHash(msg.Block.Header.ShardID, msg.SourceAndShardTxHashMap)
			}
		case <-self.quitC:
			return
		}
	}
}

func (self *ChainManager) saveSourceAndShardTxHash(shardID common.ShardID, sourceAndShardTxHash map[common.Uint256]common.Uint256) {
	lgr := ledger.GetShardLedger(shardID)
	if lgr == nil {
		log.Error("lgr is nil")
		return
	}
	for sourceTxHash, shardTxHash := range sourceAndShardTxHash {
		err := lgr.SaveShardTxHashWithSourceTxHash(sourceTxHash, shardTxHash)
		if err != nil {
			log.Errorf("[saveSourceAndShardTxHash] error: %s", err)
		}
	}
}

//crossShardEventLoop: process all shard to shard msg
func (self *ChainManager) crossShardEventLoop() {
	self.quitWg.Add(1)
	defer self.quitWg.Done()
	for {
		select {
		case msg := <-self.crossShardMsgC:
			self.handleCrossShardMsg(msg)
		case <-self.quitC:
			return
		}
	}
}

func (self *ChainManager) Close() {
	close(self.quitC)
	self.quitWg.Wait()
}

func (self *ChainManager) Stop() {
	// TODO
}
