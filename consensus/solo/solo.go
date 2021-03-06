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

package solo

import (
	"fmt"
	"reflect"
	"time"

	"github.com/ontio/ontology-crypto/keypair"
	"github.com/ontio/ontology-eventbus/actor"
	"github.com/ontio/ontology/account"
	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/config"
	"github.com/ontio/ontology/common/log"
	actorTypes "github.com/ontio/ontology/consensus/actor"
	"github.com/ontio/ontology/consensus/utils"
	"github.com/ontio/ontology/core/chainmgr/xshard"
	"github.com/ontio/ontology/core/ledger"
	"github.com/ontio/ontology/core/signature"
	com "github.com/ontio/ontology/core/store/common"
	"github.com/ontio/ontology/core/types"
	"github.com/ontio/ontology/core/xshard_types"
	"github.com/ontio/ontology/events"
	"github.com/ontio/ontology/events/message"
	p2pmsg "github.com/ontio/ontology/p2pserver/message/types"
	"github.com/ontio/ontology/validator/increment"
)

/*
*Simple consensus for solo node in test environment.
 */
const ContextVersion uint32 = common.CURR_HEADER_VERSION

type SoloService struct {
	Account          *account.Account
	poolActor        *actorTypes.TxPoolActor
	incrValidator    *increment.IncrementValidator
	existCh          chan interface{}
	genBlockInterval time.Duration
	pid              *actor.PID
	sub              *events.ActorSubscriber
	p2p              *actorTypes.P2PActor
	// sharding
	shardID      common.ShardID
	parentHeight uint32 // ParentHeight of last block
	ledger       *ledger.Ledger
}

func NewSoloService(shardID common.ShardID, bkAccount *account.Account, txpool *actor.PID, lgr *ledger.Ledger, p2p *actor.PID) (*SoloService, error) {
	service := &SoloService{
		Account:          bkAccount,
		poolActor:        &actorTypes.TxPoolActor{Pool: txpool},
		incrValidator:    increment.NewIncrementValidator(20),
		genBlockInterval: time.Duration(config.DefConfig.Genesis.SOLO.GenBlockTime) * time.Second,
		shardID:          shardID,
		ledger:           lgr,
		p2p:              &actorTypes.P2PActor{P2P: p2p},
	}

	props := actor.FromProducer(func() actor.Actor {
		return service
	})

	pid, err := actor.SpawnNamed(props, "consensus_solo")
	service.pid = pid
	service.sub = events.NewActorSubscriber(pid)

	// load parentHeight from ledger
	blkhdr, err := lgr.GetHeaderByHeight(lgr.GetCurrentBlockHeight())
	if err != nil {
		return nil, fmt.Errorf("failed to get current block header: %s", err)
	}
	service.parentHeight = blkhdr.ParentHeight

	return service, err
}

func (self *SoloService) Receive(context actor.Context) {
	switch msg := context.Message().(type) {
	case *actor.Restarting:
		log.Info("solo actor restarting")
	case *actor.Stopping:
		log.Info("solo actor stopping")
	case *actor.Stopped:
		log.Info("solo actor stopped")
	case *actor.Started:
		log.Info("solo actor started")
	case *actor.Restart:
		log.Info("solo actor restart")
	case *actorTypes.StartConsensus:
		if self.existCh != nil {
			log.Info("consensus have started")
			return
		}

		self.sub.Subscribe(message.TOPIC_SAVE_BLOCK_COMPLETE)

		timer := time.NewTicker(self.genBlockInterval)
		self.existCh = make(chan interface{})
		go func() {
			defer timer.Stop()
			existCh := self.existCh
			for {
				select {
				case <-timer.C:
					self.pid.Tell(&actorTypes.TimeOut{})
				case <-existCh:
					return
				}
			}
		}()
	case *actorTypes.StopConsensus:
		if self.existCh != nil {
			close(self.existCh)
			self.existCh = nil
			self.incrValidator.Clean()
			self.sub.Unsubscribe(message.TOPIC_SAVE_BLOCK_COMPLETE)
		}
	case *message.SaveBlockCompleteMsg:
		log.Infof("solo actor receives block complete event from shard %d. block height=%d parent=%d txnum=%d shardTxNum=%d",
			msg.Block.Header.ShardID, msg.Block.Header.Height, msg.Block.Header.ParentHeight, len(msg.Block.Transactions),
			len(msg.Block.ShardTxs))
		if self.shardID == msg.Block.Header.ShardID {
			self.incrValidator.AddBlock(msg.Block)
		}

	case *actorTypes.TimeOut:
		err := self.genBlock()
		if err != nil {
			log.Errorf("Solo genBlock error %s", err)
		}
	default:
		log.Info("solo actor: Unknown msg ", msg, "type", reflect.TypeOf(msg))
	}
}

func (self *SoloService) GetPID() *actor.PID {
	return self.pid
}

func (self *SoloService) Start() error {
	self.pid.Tell(&actorTypes.StartConsensus{})
	return nil
}

func (self *SoloService) Halt() error {
	self.pid.Tell(&actorTypes.StopConsensus{})
	return nil
}

func (self *SoloService) genBlock() error {
	block, err := self.makeBlock()
	if err != nil {
		return fmt.Errorf("makeBlock error %s", err)
	}

	// parentHeight order consistency check
	if self.parentHeight > block.Header.ParentHeight {
		return fmt.Errorf("invalid parent height: %d vs %d", self.parentHeight, block.Header.ParentHeight)
	}
	result, err := self.ledger.ExecuteBlock(block)
	if err != nil {
		return fmt.Errorf("executeBlock height:%d error:%s", block.Header.Height, err)
	}
	err = self.ledger.SubmitBlock(block, result)
	if err != nil {
		return fmt.Errorf("submitBlock height:%d error:%s", block.Header.Height, err)
	}
	xshard.DelCrossShardTxs(self.ledger, block.ShardTxs)
	self.broadcastCrossShardMsgs(block.Header.Height, result.ShardNotify)
	// new block persisted, update parentHeight
	self.parentHeight = block.Header.ParentHeight
	return nil
}

func (self *SoloService) broadcastCrossShardMsgs(blkNum uint32, shardMsgs []xshard_types.CommonShardMsg) {
	crossShardMsgs, hashRoot, err := utils.BuildCrossShardMsgs(self.Account, self.ledger, blkNum, shardMsgs, nil)
	if err != nil {
		log.Errorf("%s", err)
		return
	}

	for targetShardID, crossShardMsg := range crossShardMsgs {
		if targetShardID.ParentID() == self.shardID {
			continue
		}
		// get last shard-msg-root of the target shard
		prevMsgHash, err := self.ledger.GetShardMsgHash(targetShardID)
		if err != nil && err != com.ErrNotFound {
			log.Errorf("SendCrossShardMsgToAll getshardmsghash err:%s", err)
			return
		}
		// save shard-msg-root
		err = self.ledger.SaveShardMsgHash(targetShardID, hashRoot)
		if err != nil {
			log.Errorf("SaveShardMsgHash shardID:%v,msgHash:%s,err:%s", targetShardID, hashRoot.ToHexString(), err)
			return
		}
		// save cross-shard-msg
		err = self.ledger.SaveCrossShardMsgByHash(prevMsgHash, crossShardMsg)
		if err != nil {
			log.Errorf("SaveCrossShardMsgByHash preMsgHash:%s,err:%s", prevMsgHash.ToHexString(), err)
			return
		}

		// broadcast
		sink := common.ZeroCopySink{}
		crossShardMsg.Serialization(&sink)
		msg := &p2pmsg.CrossShardPayload{
			Version: common.VERSION_SUPPORT_SHARD,
			ShardID: targetShardID,
			Data:    sink.Bytes(),
		}
		self.p2p.Broadcast(msg)
	}
}

func (self *SoloService) makeBlock() (*types.Block, error) {
	log.Debug()
	owner := self.Account.PublicKey
	nextBookkeeper, err := types.AddressFromBookkeepers([]keypair.PublicKey{owner})
	if err != nil {
		return nil, fmt.Errorf("GetBookkeeperAddress error:%s", err)
	}
	prevHash := self.ledger.GetCurrentBlockHash()
	height := self.ledger.GetCurrentBlockHeight()

	validHeight := height

	start, end := self.incrValidator.BlockRange()

	if height+1 == end {
		validHeight = start
	} else {
		self.incrValidator.Clean()
		log.Infof("increment validator block height %v != ledger block height %v", int(end)-1, height)
	}

	log.Infof("current block height %v, increment validator block cache range: [%d, %d)", height, start, end)
	txs := self.poolActor.GetTxnPool(true, validHeight)

	transactions := make([]*types.Transaction, 0, len(txs))
	for _, txEntry := range txs {
		// TODO optimize to use height in txentry
		if err := self.incrValidator.Verify(txEntry.Tx, validHeight); err == nil {
			transactions = append(transactions, txEntry.Tx)
		}
	}
	txHash := []common.Uint256{}
	for _, t := range transactions {
		txHash = append(txHash, t.Hash())
	}
	txRoot := common.ComputeMerkleRoot(txHash)

	blockRoot := self.ledger.GetBlockRootWithNewTxRoots(height+1, []common.Uint256{txRoot})

	// get ParentHeight for new block
	parentHeight := self.parentHeight
	if self.ledger.GetParentHeight() >= parentHeight+uint32(config.DefConfig.Shard.ParentHeightIncrement) {
		parentHeight = parentHeight + uint32(config.DefConfig.Shard.ParentHeightIncrement)
	} else {
		parentHeight = self.ledger.GetParentHeight()
	}
	// get Cross-Shard Txs from chain-mgr
	shardTxs, err := xshard.GetCrossShardTxs(self.ledger, self.Account, self.shardID, self.parentHeight, parentHeight)
	if err != nil {
		log.Errorf("GetCrossShardTxs err:%s", err)
	}
	header := &types.Header{
		Version:          ContextVersion,
		ShardID:          self.shardID,
		ParentHeight:     uint32(parentHeight),
		PrevBlockHash:    prevHash,
		TransactionsRoot: txRoot,
		BlockRoot:        blockRoot,
		Timestamp:        uint32(time.Now().Unix()),
		Height:           height + 1,
		ConsensusData:    common.GetNonce(),
		NextBookkeeper:   nextBookkeeper,
	}
	block := &types.Block{
		Header:       header,
		ShardTxs:     shardTxs,     // Cross-Shard Txs
		Transactions: transactions, // Intra-Shard Txs
	}

	blockHash := block.Hash()

	sig, err := signature.Sign(self.Account, blockHash[:])
	if err != nil {
		return nil, fmt.Errorf("[Signature],Sign error:%s.", err)
	}

	block.Header.Bookkeepers = []keypair.PublicKey{owner}
	block.Header.SigData = [][]byte{sig}
	return block, nil
}
