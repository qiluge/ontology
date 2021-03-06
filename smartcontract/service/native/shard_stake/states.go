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

package shard_stake

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ontio/ontology/common"
)

type View uint32 // shard consensus epoch index

type XShardFeeInfo struct {
	Debt   map[common.ShardID]map[View]uint64
	Income map[common.ShardID]map[View]uint64
}

type JsonXShardFeeInfo struct {
	Debt   map[uint64]map[View]uint64
	Income map[uint64]map[View]uint64
}

func (self XShardFeeInfo) MarshalJSON() ([]byte, error) {
	jsonFeeInfo := &JsonXShardFeeInfo{
		Debt:   make(map[uint64]map[View]uint64),
		Income: make(map[uint64]map[View]uint64),
	}
	for shard, info := range self.Debt {
		jsonFeeInfo.Debt[shard.ToUint64()] = info
	}
	for shard, info := range self.Income {
		jsonFeeInfo.Income[shard.ToUint64()] = info
	}
	return json.Marshal(jsonFeeInfo)
}

func (self *XShardFeeInfo) UnmarshalJSON(input []byte) error {
	jsonFeeInfo := &JsonXShardFeeInfo{
		Debt:   make(map[uint64]map[View]uint64),
		Income: make(map[uint64]map[View]uint64),
	}
	err := json.Unmarshal(input, jsonFeeInfo)
	if err != nil {
		return err
	}
	self.Debt = make(map[common.ShardID]map[View]uint64)
	self.Income = make(map[common.ShardID]map[View]uint64)
	for shardId, info := range jsonFeeInfo.Debt {
		shard, err := common.NewShardID(shardId)
		if err != nil {
			return fmt.Errorf("generate shardId failed, err: %s", err)
		}
		self.Debt[shard] = info
	}
	for shardId, info := range jsonFeeInfo.Income {
		shard, err := common.NewShardID(shardId)
		if err != nil {
			return fmt.Errorf("generate shardId failed, err: %s", err)
		}
		self.Income[shard] = info
	}
	return nil
}

func (this *XShardFeeInfo) Serialization(sink *common.ZeroCopySink) {
	sink.WriteUint64(uint64(len(this.Debt)))
	debtShards := make([]common.ShardID, 0)
	for shard := range this.Debt {
		debtShards = append(debtShards, shard)
	}
	sort.SliceStable(debtShards, func(i, j int) bool {
		return debtShards[i].ToUint64() < debtShards[j].ToUint64()
	})
	for _, shard := range debtShards {
		sink.WriteShardID(shard)
		debts := this.Debt[shard]
		sink.WriteUint64(uint64(len(debts)))
		views := make([]View, 0)
		for view := range debts {
			views = append(views, view)
		}
		sort.SliceStable(views, func(i, j int) bool {
			return views[i] < views[j]
		})
		for _, view := range views {
			sink.WriteUint32(uint32(view))
			sink.WriteUint64(debts[view])
		}
	}
	sink.WriteUint64(uint64(len(this.Income)))
	incomeShards := make([]common.ShardID, 0)
	for shard := range this.Income {
		incomeShards = append(incomeShards, shard)
	}
	sort.SliceStable(incomeShards, func(i, j int) bool {
		return incomeShards[i].ToUint64() < incomeShards[j].ToUint64()
	})
	for _, shard := range incomeShards {
		sink.WriteShardID(shard)
		incomes := this.Income[shard]
		sink.WriteUint64(uint64(len(incomes)))
		views := make([]View, 0)
		for view := range incomes {
			views = append(views, view)
		}
		sort.SliceStable(views, func(i, j int) bool {
			return views[i] < views[j]
		})
		for _, view := range views {
			sink.WriteUint32(uint32(view))
			sink.WriteUint64(incomes[view])
		}
	}
}

func (this *XShardFeeInfo) Deserialization(source *common.ZeroCopySource) error {
	debtNum, eof := source.NextUint64()
	if eof {
		return io.ErrUnexpectedEOF
	}
	this.Debt = make(map[common.ShardID]map[View]uint64)
	for i := uint64(0); i < debtNum; i++ {
		shard, err := source.NextShardID()
		if err != nil {
			return fmt.Errorf("deserialization: read debt shardId failed, err: %s", err)
		}
		num, eof := source.NextUint64()
		if eof {
			return io.ErrUnexpectedEOF
		}
		viewFeeInfo := make(map[View]uint64)
		for i := uint64(0); i < num; i++ {
			view, eof := source.NextUint32()
			fee, eof := source.NextUint64()
			if eof {
				return fmt.Errorf("deserialization: read debt view fee failed, shard %d, index %d, err: %s",
					shard.ToUint64(), i, io.ErrUnexpectedEOF)
			}
			viewFeeInfo[View(view)] = fee
		}
		this.Debt[shard] = viewFeeInfo
	}
	incomeNum, eof := source.NextUint64()
	if eof {
		return io.ErrUnexpectedEOF
	}
	this.Income = make(map[common.ShardID]map[View]uint64)
	for i := uint64(0); i < incomeNum; i++ {
		shard, err := source.NextShardID()
		if err != nil {
			return fmt.Errorf("deserialization: read income shardId failed, err: %s", err)
		}
		num, eof := source.NextUint64()
		if eof {
			return io.ErrUnexpectedEOF
		}
		viewFeeInfo := make(map[View]uint64)
		for i := uint64(0); i < num; i++ {
			view, eof := source.NextUint32()
			fee, eof := source.NextUint64()
			if eof {
				return fmt.Errorf("deserialization: read income view fee failed, shard %d, index %d, err: %s",
					shard.ToUint64(), i, io.ErrUnexpectedEOF)
			}
			viewFeeInfo[View(view)] = fee
		}
		this.Income[shard] = viewFeeInfo
	}
	return nil
}

type PeerViewInfo struct {
	PeerPubKey             string
	Owner                  common.Address
	CanStake               bool   // if user can stake peer
	WholeFee               uint64 // each epoch handling fee
	FeeBalance             uint64 // each epoch handling fee not be withdrawn
	InitPos                uint64 // node stake amount
	UserUnfreezeAmount     uint64 // all user can withdraw amount
	CurrentViewStakeAmount uint64 // current view user stake amount
	UserStakeAmount        uint64 // user stake amount
	MaxAuthorization       uint64 // max user stake amount
	Proportion             uint64 // proportion to user
}

func (this *PeerViewInfo) Serialization(sink *common.ZeroCopySink) {
	sink.WriteString(this.PeerPubKey)
	sink.WriteAddress(this.Owner)
	sink.WriteBool(this.CanStake)
	sink.WriteUint64(this.WholeFee)
	sink.WriteUint64(this.FeeBalance)
	sink.WriteUint64(this.InitPos)
	sink.WriteUint64(this.UserUnfreezeAmount)
	sink.WriteUint64(this.CurrentViewStakeAmount)
	sink.WriteUint64(this.UserStakeAmount)
	sink.WriteUint64(this.MaxAuthorization)
	sink.WriteUint64(this.Proportion)
}

func (this *PeerViewInfo) Deserialization(source *common.ZeroCopySource) error {
	var eof, irregular bool
	this.PeerPubKey, _, irregular, eof = source.NextString()
	if irregular {
		return common.ErrIrregularData
	}
	this.Owner, eof = source.NextAddress()
	this.CanStake, irregular, eof = source.NextBool()
	if irregular {
		return common.ErrIrregularData
	}
	this.WholeFee, eof = source.NextUint64()
	this.FeeBalance, eof = source.NextUint64()
	this.InitPos, eof = source.NextUint64()
	this.UserUnfreezeAmount, eof = source.NextUint64()
	this.CurrentViewStakeAmount, eof = source.NextUint64()
	this.UserStakeAmount, eof = source.NextUint64()
	this.MaxAuthorization, eof = source.NextUint64()
	this.Proportion, eof = source.NextUint64()
	if eof {
		return io.ErrUnexpectedEOF
	}
	return nil
}

type ViewInfo struct {
	Peers map[string]*PeerViewInfo
}

func (this *ViewInfo) Serialization(sink *common.ZeroCopySink) {
	sink.WriteUint64(uint64(len(this.Peers)))
	peerInfoList := make([]*PeerViewInfo, 0)
	for _, info := range this.Peers {
		peerInfoList = append(peerInfoList, info)
	}
	sort.SliceStable(peerInfoList, func(i, j int) bool {
		return peerInfoList[i].PeerPubKey > peerInfoList[j].PeerPubKey
	})
	for _, peer := range peerInfoList {
		peer.Serialization(sink)
	}
}

func (this *ViewInfo) Deserialization(source *common.ZeroCopySource) error {
	num, eof := source.NextUint64()
	if eof {
		return io.ErrUnexpectedEOF
	}
	this.Peers = make(map[string]*PeerViewInfo)
	for i := uint64(0); i < num; i++ {
		peer := &PeerViewInfo{}
		if err := peer.Deserialization(source); err != nil {
			return fmt.Errorf("index %d, err: %s", i, err)
		}
		this.Peers[strings.ToLower(peer.PeerPubKey)] = peer
	}
	return nil
}

type UserPeerStakeInfo struct {
	PeerPubKey             string
	StakeAmount            uint64
	CurrentViewStakeAmount uint64
	UnfreezeAmount         uint64
}

func (this *UserPeerStakeInfo) Serialization(sink *common.ZeroCopySink) {
	sink.WriteString(this.PeerPubKey)
	sink.WriteUint64(this.StakeAmount)
	sink.WriteUint64(this.CurrentViewStakeAmount)
	sink.WriteUint64(this.UnfreezeAmount)
}

func (this *UserPeerStakeInfo) Deserialization(source *common.ZeroCopySource) error {
	var eof, irregular bool
	this.PeerPubKey, _, irregular, eof = source.NextString()
	if irregular {
		return common.ErrIrregularData
	}
	this.StakeAmount, eof = source.NextUint64()
	this.CurrentViewStakeAmount, eof = source.NextUint64()
	this.UnfreezeAmount, eof = source.NextUint64()
	if eof {
		return io.ErrUnexpectedEOF
	}
	return nil
}

type UserStakeInfo struct {
	Peers map[string]*UserPeerStakeInfo
}

func (this *UserStakeInfo) Serialization(sink *common.ZeroCopySink) {
	sink.WriteUint64(uint64(len(this.Peers)))
	peerInfoList := make([]*UserPeerStakeInfo, 0)
	for _, info := range this.Peers {
		peerInfoList = append(peerInfoList, info)
	}
	sort.SliceStable(peerInfoList, func(i, j int) bool {
		return peerInfoList[i].PeerPubKey > peerInfoList[j].PeerPubKey
	})
	for _, peer := range peerInfoList {
		peer.Serialization(sink)
	}
}

func (this *UserStakeInfo) Deserialization(source *common.ZeroCopySource) error {
	num, eof := source.NextUint64()
	if eof {
		return io.ErrUnexpectedEOF
	}
	this.Peers = make(map[string]*UserPeerStakeInfo)
	for i := uint64(0); i < num; i++ {
		peer := &UserPeerStakeInfo{}
		if err := peer.Deserialization(source); err != nil {
			return fmt.Errorf("index %d, err: %s", i, err)
		}
		this.Peers[strings.ToLower(peer.PeerPubKey)] = peer
	}
	return nil
}

type UserUnboundOngInfo struct {
	Time        uint32
	StakeAmount uint64
	Balance     uint64
}

func (this *UserUnboundOngInfo) Serialization(sink *common.ZeroCopySink) {
	sink.WriteUint32(this.Time)
	sink.WriteUint64(this.StakeAmount)
	sink.WriteUint64(this.Balance)
}

func (this *UserUnboundOngInfo) Deserialization(source *common.ZeroCopySource) error {
	var eof bool
	this.Time, eof = source.NextUint32()
	this.StakeAmount, eof = source.NextUint64()
	this.Balance, eof = source.NextUint64()
	if eof {
		return io.ErrUnexpectedEOF
	}
	return nil
}
