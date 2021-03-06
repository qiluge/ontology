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

package shardmgmt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/config"
	"github.com/ontio/ontology/common/log"
	"github.com/ontio/ontology/common/serialization"
	"github.com/ontio/ontology/smartcontract/service/native"
	"github.com/ontio/ontology/smartcontract/service/native/global_params"
	"github.com/ontio/ontology/smartcontract/service/native/ong"
	"github.com/ontio/ontology/smartcontract/service/native/ont"
	"github.com/ontio/ontology/smartcontract/service/native/shard_stake"
	"github.com/ontio/ontology/smartcontract/service/native/shardasset/oep4"
	"github.com/ontio/ontology/smartcontract/service/native/shardmgmt/states"
	"github.com/ontio/ontology/smartcontract/service/native/utils"
)

/////////
//
// Shard management contract
//
//	. create shard
//	. config shard
//	. join shard
//	. activate shard
//
/////////

const (
	// only can be invoked by shard chain operator
	INIT_NAME               = "init"
	SET_MGMT_SHARD_FEE_ADDR = "setMgmtShardFeeAddr"
	SET_CREATE_SHARD_FEE    = "setCreateShardFee"
	SET_JOIN_SHARD_FEE      = "setJoinShardFee"

	CREATE_SHARD_NAME        = "createShard"
	CONFIG_SHARD_NAME        = "configShard"
	APPLY_JOIN_SHARD_NAME    = "applyJoinShard"
	APPROVE_JOIN_SHARD_NAME  = "approveJoinShard"
	JOIN_SHARD_NAME          = "joinShard"
	EXIT_SHARD_NAME          = "exitShard"
	ACTIVATE_SHARD_NAME      = "activateShard"
	NOTIFY_SHARD_COMMIT_DPOS = "notifyShardCommitDpos"
	UPDATE_CONFIG            = "updateConfig"

	NOTIFY_PARENT_COMMIT_DPOS  = "notifyParentCommitDpos"
	COMMIT_DPOS_NAME           = "commitDpos"
	SHARD_COMMIT_DPOS          = "shardCommitDpos"
	SHARD_RETRY_COMMIT_DPOS    = "shardRetryCommitDpos"
	UPDATE_XSHARD_HANDLING_FEE = "updateXShardHandlingFee"

	// query shard commit Dpos info, include xshard transfer ong
	// id, commit dpos height and block hash at shard, and whole handling fee at last consensus epoch at shard
	GET_SHARD_COMMIT_DPOS_INFO = "getShardCommitDPosInfo"
	// query shard detail after create it
	GET_SHARD_DETAIL = "getShardDetail"
)

func InitShardManagement() {
	native.Contracts[utils.ShardMgmtContractAddress] = RegisterShardMgmtContract
}

func RegisterShardMgmtContract(native *native.NativeService) {
	native.Register(INIT_NAME, ShardMgmtInit)
	native.Register(SET_MGMT_SHARD_FEE_ADDR, SetMgmtShardFeeAddr)
	native.Register(SET_CREATE_SHARD_FEE, SetCreateShardFee)
	native.Register(SET_JOIN_SHARD_FEE, SetJoinShardFee)

	native.Register(CREATE_SHARD_NAME, CreateShard)
	native.Register(CONFIG_SHARD_NAME, ConfigShard)
	native.Register(APPLY_JOIN_SHARD_NAME, ApplyJoinShard)
	native.Register(APPROVE_JOIN_SHARD_NAME, ApproveJoinShard)
	native.Register(JOIN_SHARD_NAME, JoinShard)
	native.Register(ACTIVATE_SHARD_NAME, ActivateShard)
	native.Register(EXIT_SHARD_NAME, ExitShard)
	native.Register(NOTIFY_SHARD_COMMIT_DPOS, NotifyShardCommitDpos)
	native.Register(UPDATE_CONFIG, UpdateConfig)

	native.Register(NOTIFY_PARENT_COMMIT_DPOS, NotifyParentCommitDpos)
	native.Register(COMMIT_DPOS_NAME, CommitDpos)
	native.Register(SHARD_COMMIT_DPOS, ShardCommitDpos)
	native.Register(SHARD_RETRY_COMMIT_DPOS, ShardRetryCommitDpos)
	native.Register(UPDATE_XSHARD_HANDLING_FEE, UpdateXShardHandlingFee)

	native.Register(GET_SHARD_COMMIT_DPOS_INFO, GetShardCommitDPosInfo)
	native.Register(GET_SHARD_DETAIL, GetShardDetail)
}

func ShardMgmtInit(native *native.NativeService) ([]byte, error) {
	operator, err := global_params.GetStorageRole(native,
		global_params.GenerateOperatorKey(utils.ParamContractAddress))
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardMgmtInit: get admin error: %v", err)
	}
	if err := utils.ValidateOwner(native, operator); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardMgmtInit: checkWitness error: %v", err)
	}

	contract := native.ContextRef.CurrentContext().ContractAddress
	// check if shard-mgmt initialized
	ver, err := getVersion(native, contract)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("init shard mgmt, get version: %s", err)
	}
	if ver == 0 {
		// initialize shardmgmt version
		setVersion(native, contract)
		param := &InitShardParam{}
		if err := param.Deserialize(bytes.NewReader(native.Input)); err != nil {
			log.Debugf("ShardMgmtInit: %s, use default init param at shard %d", err, native.ShardID.ToUint64())
			param = &InitShardParam{
				MgmtShardFeeAddr: utils.OngContractAddress,
				CreateShardFee:   new(big.Int).SetUint64(shardstates.DEFAULT_CREATE_SHARD_FEE),
				JoinShardFee:     new(big.Int).SetUint64(shardstates.DEFAULT_JOIN_SHARD_FEE),
			}
		}
		setMgmtShardFeeAddr(native, param.MgmtShardFeeAddr)
		setCreateShardFee(native, param.CreateShardFee)
		setJoinShardFee(native, param.JoinShardFee)
		// initialize shard mgmt
		globalState := &shardstates.ShardMgmtGlobalState{NextSubShardIndex: 1}
		setGlobalState(native, contract, globalState)

		// initialize shard states
		shardState := &shardstates.ShardState{
			ShardID:             native.ShardID,
			GenesisParentHeight: native.Height,
			State:               shardstates.SHARD_STATE_ACTIVE,
			Config:              &shardstates.ShardConfig{VbftCfg: &config.VBFTConfig{}},
			Peers:               make(map[string]*shardstates.PeerShardStakeInfo),
		}
		setShardState(native, contract, shardState)
		return utils.BYTE_TRUE, nil
	}

	if ver < utils.VERSION_CONTRACT_SHARD_MGMT {
		// make upgrade
		return utils.BYTE_FALSE, fmt.Errorf("upgrade TBD")
	} else if ver > utils.VERSION_CONTRACT_SHARD_MGMT {
		return utils.BYTE_FALSE, fmt.Errorf("version downgrade from %d to %d", ver, utils.VERSION_CONTRACT_SHARD_MGMT)
	}

	return utils.BYTE_TRUE, nil
}

func CreateShard(native *native.NativeService) ([]byte, error) {
	params := new(CreateShardParam)
	if err := params.Deserialize(bytes.NewBuffer(native.Input)); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("create shard, invalid param: %s", err)
	}
	if params.ParentShardID.ToUint64() != 0 {
		return utils.BYTE_FALSE, fmt.Errorf("create shard, invalid parent shard: %d", params.ParentShardID)
	}
	if params.ParentShardID != native.ShardID {
		return utils.BYTE_FALSE, fmt.Errorf("CreateShard: parent ShardID is not current shard")
	}

	if err := utils.ValidateOwner(native, params.Creator); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("CreateShard: invalid creator: %s", err)
	}

	contract := native.ContextRef.CurrentContext().ContractAddress
	if ok, err := checkVersion(native, contract); !ok || err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("CreateShard: check version: %s", err)
	}

	globalState, err := getGlobalState(native, contract)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("CreateShard: get global state: %s", err)
	}

	subShardID, err := native.ShardID.GenSubShardID(globalState.NextSubShardIndex)
	if err != nil {
		return utils.BYTE_FALSE, err
	}

	shard := &shardstates.ShardState{
		ShardID: subShardID,
		Creator: params.Creator,
		State:   shardstates.SHARD_STATE_CREATED,
		Config:  &shardstates.ShardConfig{VbftCfg: &config.VBFTConfig{}},
		Peers:   make(map[string]*shardstates.PeerShardStakeInfo),
	}
	globalState.NextSubShardIndex += 1

	// update global state
	setGlobalState(native, contract, globalState)
	// save shard
	setShardState(native, contract, shard)
	// charge create shard fee
	if err := chargeShardMgmtFee(native, shardstates.TYPE_CREATE_SHARD_FEE, params.Creator); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("CreateShard: failed, err: %s", err)
	}

	evt := &shardstates.CreateShardEvent{
		SourceShardID: native.ShardID,
		Height:        native.Height,
		NewShardID:    shard.ShardID,
	}
	AddNotification(native, contract, evt)
	return utils.BYTE_TRUE, nil
}

func ConfigShard(native *native.NativeService) ([]byte, error) {
	params := new(ConfigShardParam)
	if err := params.Deserialize(bytes.NewBuffer(native.Input)); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("config shard, invalid param: %s", err)
	}

	contract := native.ContextRef.CurrentContext().ContractAddress
	if ok, err := checkVersion(native, contract); !ok || err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ConfigShard: check version: %s", err)
	}

	shard, err := GetShardState(native, contract, params.ShardID)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ConfigShard: get shard: %s", err)
	}

	if shard.State != shardstates.SHARD_STATE_CONFIGURED && shard.State != shardstates.SHARD_STATE_CREATED {
		return utils.BYTE_FALSE, fmt.Errorf("ConfigShard: shard state unmatch")
	}

	if err := utils.ValidateOwner(native, shard.Creator); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ConfigShard: invalid configurator: %s", err)
	}
	if shard.ShardID.ParentID() != native.ShardID {
		return utils.BYTE_FALSE, fmt.Errorf("ConfigShard: not on parent shard")
	}

	if params.NetworkMin < 1 {
		return utils.BYTE_FALSE, fmt.Errorf("ConfigShard: invalid shard network size")
	}

	// TODO: reset default values
	if params.GasPrice == 0 && params.GasLimit == 0 {
		params.GasPrice = 500
		params.GasLimit = 200000
	}

	// TODO: support other stake
	if params.StakeAssetAddress.ToHexString() != utils.OntContractAddress.ToHexString() {
		return utils.BYTE_FALSE, fmt.Errorf("ConfigShard: only support ONT staking")
	}
	if params.GasAssetAddress.ToHexString() != utils.OngContractAddress.ToHexString() {
		return utils.BYTE_FALSE, fmt.Errorf("ConfigShard: only support ONG gas")
	}

	shard.Config = &shardstates.ShardConfig{
		NetworkSize:       params.NetworkMin,
		StakeAssetAddress: params.StakeAssetAddress,
		GasAssetAddress:   params.GasAssetAddress,
		GasPrice:          params.GasPrice,
		GasLimit:          params.GasLimit,
	}
	cfg, err := params.GetConfig()
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ConfigShard: decode config failed, err: %s", err)
	}
	if err := utils.CheckVBFTConfig(cfg); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ConfigShard: failed, err: %s", err)
	}
	shard.Config.VbftCfg = cfg
	shard.State = shardstates.SHARD_STATE_CONFIGURED

	if err := initStakeContractShard(native, params.ShardID, uint64(cfg.MinInitStake), params.StakeAssetAddress); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ConfigShard: failed, err: %s", err)
	}
	setShardState(native, contract, shard)

	evt := &shardstates.ConfigShardEvent{
		Height: native.Height,
		Config: shard.Config,
		Peers:  shard.Peers,
	}
	evt.SourceShardID = native.ShardID
	evt.ShardID = native.ShardID
	AddNotification(native, contract, evt)
	return utils.BYTE_TRUE, nil
}

func ApplyJoinShard(native *native.NativeService) ([]byte, error) {
	params := new(ApplyJoinShardParam)
	if err := params.Deserialize(bytes.NewBuffer(native.Input)); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ApplyJoinShard: invalid param: %s", err)
	}
	if err := utils.ValidateOwner(native, params.PeerOwner); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ApplyJoinShard: check witness faield, err: %s", err)
	}
	// verify peer is exist in root chain consensus
	if config.DefConfig.Genesis.ConsensusType == config.CONSENSUS_TYPE_VBFT {
		if parentPeer, err := getRootCurrentViewPeerItem(native, params.PeerPubKey); err != nil {
			return utils.BYTE_FALSE, fmt.Errorf("ApplyJoinShard: failed, err: %s", err)
		} else if parentPeer.Address != params.PeerOwner {
			return utils.BYTE_FALSE, fmt.Errorf("ApplyJoinShard: peer owner unmatch")
		}
	}

	contract := native.ContextRef.CurrentContext().ContractAddress
	shard, err := GetShardState(native, contract, params.ShardId)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ApplyJoinShard: get shard: %s", err)
	}
	if shard.State < shardstates.SHARD_STATE_CONFIGURED {
		return utils.BYTE_FALSE, fmt.Errorf("ApplyJoinShard: shard state unmatch")
	}
	state, err := getShardPeerState(native, contract, params.ShardId, params.PeerPubKey)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ApplyJoinShard: faile, err: %s", err)
	}
	if state != state_default {
		return utils.BYTE_FALSE, fmt.Errorf("ApplyJoinShard: peer %s has already applied", params.PeerPubKey)
	}
	setShardPeerState(native, contract, params.ShardId, state_applied, params.PeerPubKey)
	return utils.BYTE_TRUE, nil
}

func ApproveJoinShard(native *native.NativeService) ([]byte, error) {
	params := new(ApproveJoinShardParam)
	if err := params.Deserialize(bytes.NewBuffer(native.Input)); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ApproveJoinShard: invalid param: %s", err)
	}
	contract := native.ContextRef.CurrentContext().ContractAddress
	shard, err := GetShardState(native, contract, params.ShardId)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ApproveJoinShard: cannot get shard %d, err: %s", params.ShardId, err)
	}
	if err := utils.ValidateOwner(native, shard.Creator); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ApproveJoinShard: check witness failed, err: %s", err)
	}
	for _, pubKey := range params.PeerPubKey {
		state, err := getShardPeerState(native, contract, params.ShardId, pubKey)
		if err != nil {
			return utils.BYTE_FALSE, fmt.Errorf("ApproveJoinShard: faile, err: %s", err)
		}
		if state != state_applied {
			return utils.BYTE_FALSE, fmt.Errorf("ApproveJoinShard: peer %s hasn't applied", pubKey)
		}
		setShardPeerState(native, contract, params.ShardId, state_approved, pubKey)
	}
	return utils.BYTE_TRUE, nil
}

func JoinShard(native *native.NativeService) ([]byte, error) {
	params := new(JoinShardParam)
	if err := params.Deserialize(bytes.NewBuffer(native.Input)); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("join shard, invalid param: %s", err)
	}

	if err := utils.ValidateOwner(native, params.PeerOwner); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("JoinShard: invalid peer owner: %s", err)
	}

	contract := native.ContextRef.CurrentContext().ContractAddress
	if ok, err := checkVersion(native, contract); !ok || err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("JoinShard: check version: %s", err)
	}

	shard, err := GetShardState(native, contract, params.ShardID)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("JoinShard: get shard: %s", err)
	}
	if shard.ShardID.ParentID() != native.ShardID {
		return utils.BYTE_FALSE, fmt.Errorf("JoinShard: not on parent shard")
	}

	peerIndex := uint32(len(shard.Peers) + 1)
	if config.DefConfig.Genesis.ConsensusType == config.CONSENSUS_TYPE_VBFT {
		rootChainPeerItem, err := getRootCurrentViewPeerItem(native, params.PeerPubKey)
		if err != nil {
			return utils.BYTE_FALSE, fmt.Errorf("JoinShard: failed, err: %s", err)
		}
		if rootChainPeerItem.InitPos < params.StakeAmount {
			return utils.BYTE_FALSE, fmt.Errorf("JoinShard: shard stake amount should less than root chain")
		}
		if rootChainPeerItem.Address != params.PeerOwner {
			return utils.BYTE_FALSE, fmt.Errorf("JoinShard: peer owner unmatch")
		}
		peerIndex = rootChainPeerItem.Index
	}

	state, err := getShardPeerState(native, contract, params.ShardID, params.PeerPubKey)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("JoinShard: failed, err: %s", err)
	}
	if state != state_approved {
		return utils.BYTE_FALSE, fmt.Errorf("JoinShard: peer state %s unmatch", state)
	}
	setShardPeerState(native, contract, params.ShardID, state_joined, params.PeerPubKey)

	// charge join shard fee
	if err := chargeShardMgmtFee(native, shardstates.TYPE_CREATE_SHARD_FEE, params.PeerOwner); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("JoinShard: failed, err: %s", err)
	}

	if _, present := shard.Peers[strings.ToLower(params.PeerPubKey)]; present {
		return utils.BYTE_FALSE, fmt.Errorf("JoinShard: peer already in shard")
	} else if len(shard.Peers) >= math.MaxUint32-1 { // peer index is max uint32 and start from 1
		return utils.BYTE_FALSE, fmt.Errorf("JoinShard: peers num exceed")
	} else {
		if shard.Peers == nil {
			shard.Peers = make(map[string]*shardstates.PeerShardStakeInfo)
		}
		peerStakeInfo := &shardstates.PeerShardStakeInfo{
			Index:      peerIndex,
			IpAddress:  params.IpAddress,
			PeerOwner:  params.PeerOwner,
			PeerPubKey: params.PeerPubKey,
		}
		shard.Peers[strings.ToLower(params.PeerPubKey)] = peerStakeInfo
	}
	// peer would join after shard activate
	isShardActivate := shard.State == shardstates.SHARD_STATE_ACTIVE
	if !isShardActivate {
		shard.State = shardstates.SHARD_PEER_JOIND
	}
	setShardState(native, contract, shard)

	// call shard stake contract
	if err := peerInitStake(native, params, isShardActivate); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("JoinShard: failed, err: %s", err)
	}

	evt := &shardstates.PeerJoinShardEvent{
		Height:     native.Height,
		PeerPubKey: params.PeerPubKey,
	}
	evt.SourceShardID = native.ShardID
	evt.ShardID = native.ShardID
	AddNotification(native, contract, evt)
	return utils.BYTE_TRUE, nil
}

func ExitShard(native *native.NativeService) ([]byte, error) {
	param := new(ExitShardParam)
	if err := param.Deserialize(bytes.NewBuffer(native.Input)); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ExitShard: invalid param: %s", err)
	}

	contract := native.ContextRef.CurrentContext().ContractAddress
	if ok, err := checkVersion(native, contract); !ok || err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ExitShard: check version: %s", err)
	}
	if err := utils.ValidateOwner(native, param.PeerOwner); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ExitShard: check witness failed, err: %s", err)
	}
	shard, err := GetShardState(native, contract, param.ShardId)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ExitShard: get shard state failed, err: %s", err)
	}
	shardPeerInfo, ok := shard.Peers[strings.ToLower(param.PeerPubKey)]
	if !ok {
		return utils.BYTE_FALSE, fmt.Errorf("ExitShard: peer not exist in shard, err: %s", err)
	}
	if shardPeerInfo.PeerOwner != param.PeerOwner {
		return utils.BYTE_FALSE, fmt.Errorf("ExitShard: peer owner unmatch")
	}
	if err := peerExit(native, param.ShardId, param.PeerPubKey); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ExitShard: failed, err: %s", err)
	}
	if shardPeerInfo.NodeType == shardstates.CONSENSUS_NODE {
		if len(shard.Peers)-1 < int(shard.Config.VbftCfg.K) &&
			config.DefConfig.Genesis.ConsensusType == config.CONSENSUS_TYPE_VBFT {
			return utils.BYTE_FALSE, fmt.Errorf("ExitShard: peer cannot exit")
		}
		shardPeerInfo.NodeType = shardstates.QUIT_CONSENSUS_NODE
	} else if shardPeerInfo.NodeType == shardstates.CONDIDATE_NODE {
		shardPeerInfo.NodeType = shardstates.QUITING_CONSENSUS_NODE
	} else {
		return utils.BYTE_FALSE, fmt.Errorf("ExitShard: peer has already exit")
	}
	shard.Peers[strings.ToLower(param.PeerPubKey)] = shardPeerInfo
	setShardState(native, contract, shard)

	return utils.BYTE_TRUE, nil
}

func ActivateShard(native *native.NativeService) ([]byte, error) {
	params := new(ActivateShardParam)
	if err := params.Deserialize(bytes.NewBuffer(native.Input)); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ActivateShard: invalid param: %s", err)
	}

	contract := native.ContextRef.CurrentContext().ContractAddress
	if ok, err := checkVersion(native, contract); !ok || err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ActivateShard: check version: %s", err)
	}

	shard, err := GetShardState(native, contract, params.ShardID)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ActivateShard: get shard: %s", err)
	}

	if err := utils.ValidateOwner(native, shard.Creator); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ActivateShard: invalid configurator: %s", err)
	}
	if shard.State != shardstates.SHARD_PEER_JOIND {
		return utils.BYTE_FALSE, fmt.Errorf("ActivateShard: invalid shard state: %d", shard.State)
	}
	if config.DefConfig.Genesis.ConsensusType == config.CONSENSUS_TYPE_VBFT &&
		len(shard.Peers) < int(shard.Config.NetworkSize) {
		return utils.BYTE_FALSE, fmt.Errorf("ActivateShard: num of peer not enough")
	}
	if shard.ShardID.ParentID() != native.ShardID {
		return utils.BYTE_FALSE, fmt.Errorf("ActivateShard: not on parent shard")
	}
	if err = shard.UpdateDposInfo(native); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ActivateShard: failed, err: %s", err)
	}
	shard.GenesisParentHeight = native.Height
	shard.State = shardstates.SHARD_STATE_ACTIVE
	setShardState(native, contract, shard)

	evt := &shardstates.ShardActiveEvent{Height: native.Height}
	evt.SourceShardID = native.ShardID
	evt.ShardID = shard.ShardID
	AddNotification(native, contract, evt)
	return utils.BYTE_TRUE, nil
}

func UpdateConfig(native *native.NativeService) ([]byte, error) {
	param := &UpdateConfigParam{}
	if err := param.Deserialize(bytes.NewReader(native.Input)); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateConfig: failed, err: %s", err)
	}
	if param.ShardId.ParentID() != native.ShardID {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateConfig: only can be invoked at parent shard")
	}
	shard, err := GetShardState(native, utils.ShardMgmtContractAddress, param.ShardId)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateConfig: failed, err: %s", err)
	}
	if shard.State != shardstates.SHARD_STATE_ACTIVE {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateConfig: shard state unmatch")
	}
	if err := utils.ValidateOwner(native, shard.Creator); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateConfig: check witness failed, err: %s", err)
	}
	if err := checkNewCfg(param.NewCfg, shard); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateConfig: failed, err: %s", err)
	}
	updateNewCfg(native, shard, param.NewCfg)
	commitDposParam := &NotifyRootCommitDPosParam{
		ShardId:     param.ShardId,
		ForceCommit: true,
	}
	bf := new(bytes.Buffer)
	if err := commitDposParam.Serialize(bf); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateConfig: failed, err: %s", err)
	}
	data := make([]byte, len(bf.Bytes()))
	copy(data, bf.Bytes())
	bf.Reset()
	if err := serialization.WriteVarBytes(bf, data); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateConfig: encage commit dpos param failed, err: %s", err)
	}
	native.Input = bf.Bytes()
	if _, err := CommitDpos(native); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateConfig: failed, err: %s", err)
	}
	return utils.BYTE_TRUE, nil
}

func SetMgmtShardFeeAddr(native *native.NativeService) ([]byte, error) {
	operator, err := global_params.GetStorageRole(native,
		global_params.GenerateOperatorKey(utils.ParamContractAddress))
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardMgmtInit: get admin error: %v", err)
	}
	if err := utils.ValidateOwner(native, operator); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardMgmtInit: checkWitness error: %v", err)
	}
	contract := native.ContextRef.CurrentContext().ContractAddress
	if ok, err := checkVersion(native, contract); !ok || err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardMgmtInit: check version: %s", err)
	}
	addr, err := utils.ReadAddress(bytes.NewReader(native.Input))
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardMgmtInit: read addr failed, err: %v", err)
	}
	setMgmtShardFeeAddr(native, addr)
	return utils.BYTE_TRUE, nil
}

func SetCreateShardFee(native *native.NativeService) ([]byte, error) {
	operator, err := global_params.GetStorageRole(native,
		global_params.GenerateOperatorKey(utils.ParamContractAddress))
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("SetCreateShardFee: get admin error: %v", err)
	}
	if err := utils.ValidateOwner(native, operator); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("SetCreateShardFee: checkWitness error: %v", err)
	}
	contract := native.ContextRef.CurrentContext().ContractAddress
	if ok, err := checkVersion(native, contract); !ok || err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("SetCreateShardFee: check version: %s", err)
	}
	fee, err := serialization.ReadVarBytes(bytes.NewReader(native.Input))
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("SetCreateShardFee: read addr failed, err: %v", err)
	}
	setCreateShardFee(native, common.BigIntFromNeoBytes(fee))
	return utils.BYTE_TRUE, nil
}

func SetJoinShardFee(native *native.NativeService) ([]byte, error) {
	operator, err := global_params.GetStorageRole(native,
		global_params.GenerateOperatorKey(utils.ParamContractAddress))
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("SetJoinShardFee: get admin error: %v", err)
	}
	if err := utils.ValidateOwner(native, operator); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("SetJoinShardFee: checkWitness error: %v", err)
	}
	contract := native.ContextRef.CurrentContext().ContractAddress
	if ok, err := checkVersion(native, contract); !ok || err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("SetJoinShardFee: check version: %s", err)
	}
	fee, err := serialization.ReadVarBytes(bytes.NewReader(native.Input))
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("SetJoinShardFee: read addr failed, err: %v", err)
	}
	setJoinShardFee(native, common.BigIntFromNeoBytes(fee))
	return utils.BYTE_TRUE, nil
}

func NotifyParentCommitDpos(native *native.NativeService) ([]byte, error) {
	if native.ShardID.IsRootShard() {
		return utils.BYTE_FALSE, fmt.Errorf("NotifyParentCommitDpos: only can be invoked at shard")
	}
	param := &NotifyRootCommitDPosParam{
		Height:      native.Height,
		ShardId:     native.ShardID,
		ForceCommit: false,
	}
	bf := new(bytes.Buffer)
	if err := param.Serialize(bf); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("NotifyParentCommitDpos: failed, err: %s", err)
	}
	native.NotifyRemoteShard(native.ShardID.ParentID(), utils.ShardMgmtContractAddress,
		native.ContextRef.GetRemainGas(), COMMIT_DPOS_NAME, bf.Bytes())
	return utils.BYTE_TRUE, nil
}

func CommitDpos(native *native.NativeService) ([]byte, error) {
	data, err := serialization.ReadVarBytes(bytes.NewReader(native.Input))
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("decode input failed, err: %s", err)
	}
	param := &NotifyRootCommitDPosParam{}
	if err = param.Deserialize(bytes.NewReader(data)); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("CommitDpos: deserialize shardId failed, err: %s", err)
	}
	if param.ShardId.ParentID() != native.ShardID {
		return utils.BYTE_FALSE, fmt.Errorf("CommitDpos: only can be invoked by child shard")
	}
	contract := native.ContextRef.CurrentContext().ContractAddress
	if ok, err := checkVersion(native, contract); !ok || err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("CommitDpos: check version: %s", err)
	}
	shardId := param.ShardId
	shard, err := GetShardState(native, contract, shardId)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("CommitDpos: get shard: %s", err)
	}
	shardCurrentView, err := shard_stake.GetShardCurrentChangeView(native, shardId)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("CommitDpos: failed, err: %s", err)
	}
	if param.ForceCommit {
		if err := utils.ValidateOwner(native, shard.Creator); err != nil {
			return utils.BYTE_FALSE, fmt.Errorf("CommitDpos: checkwitness failed, err: %s", err)
		}
	} else if !native.ContextRef.CheckCallShard(param.ShardId) {
		return utils.BYTE_FALSE, fmt.Errorf("CommitDpos: only can be invoked by ShardCall")
	} else if param.Height < shardCurrentView.Height ||
		shardCurrentView.Height > 0 && param.Height-shardCurrentView.Height < shard.Config.VbftCfg.MaxBlockChangeView ||
		shardCurrentView.Height == 0 && param.Height-shardCurrentView.Height+1 < shard.Config.VbftCfg.MaxBlockChangeView {
		return utils.BYTE_FALSE, fmt.Errorf("CommitDpos: shard height not enough")
	}
	quitPeers := make([]string, 0)
	// check peer exit shard
	for peer, info := range shard.Peers {
		if info.NodeType == shardstates.QUIT_CONSENSUS_NODE {
			info.NodeType = shardstates.QUITING_CONSENSUS_NODE
			shard.Peers[peer] = info
		} else if info.NodeType == shardstates.QUITING_CONSENSUS_NODE {
			// delete peer at mgmt contract
			delete(shard.Peers, peer)
			quitPeers = append(quitPeers, peer)
			setShardPeerState(native, contract, param.ShardId, state_default, peer)
		}
	}
	// delete peers at stake contract
	if len(quitPeers) > 0 {
		if err = deletePeer(native, shardId, quitPeers); err != nil {
			return utils.BYTE_FALSE, fmt.Errorf("CommitDpos: failed, err: %s", err)
		}
	}
	if err := preCommitDpos(native, shardId); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("CommitDpos: failed, err: %s", err)
	}
	if err := shard.UpdateDposInfo(native); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("CommitDpos: failed, err: %s", err)
	}

	evt := &shardstates.ConfigShardEvent{
		Height: native.Height,
		Config: shard.Config,
		Peers:  shard.Peers,
	}
	evt.SourceShardID = native.ShardID
	evt.ShardID = native.ShardID
	AddNotification(native, contract, evt)
	setShardState(native, contract, shard)
	native.NotifyRemoteShard(shardId, contract, native.ContextRef.GetRemainGas(), SHARD_COMMIT_DPOS, []byte{})
	return utils.BYTE_TRUE, nil
}

func NotifyShardCommitDpos(native *native.NativeService) ([]byte, error) {
	shardId, err := utils.DeserializeShardId(bytes.NewReader(native.Input))
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("NotifyShardCommitDpos: deserialize shardId failed, err: %s", err)
	}
	if shardId.ParentID() != native.ShardID {
		return utils.BYTE_TRUE, fmt.Errorf("NotifyShardCommitDpos: only can notify child shard")
	}
	contract := native.ContextRef.CurrentContext().ContractAddress
	if ok, err := checkVersion(native, contract); !ok || err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("NotifyShardCommitDpos: check version: %s", err)
	}
	shard, err := GetShardState(native, contract, shardId)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("NotifyShardCommitDpos: get shard: %s", err)
	}
	if err := utils.ValidateOwner(native, shard.Creator); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("NotifyShardCommitDpos: check witness failed, err: %s", err)
	}
	if isShardCommitting, err := shard_stake.IsShardCommitting(native, shardId); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("NotifyShardCommitDpos: failed, err: %s", err)
	} else if !isShardCommitting {
		return utils.BYTE_FALSE, fmt.Errorf("NotifyShardCommitDpos: shard isn't committing")
	}
	native.NotifyRemoteShard(shardId, contract, native.ContextRef.GetRemainGas(), SHARD_COMMIT_DPOS, []byte{})
	return utils.BYTE_TRUE, nil
}

func ShardCommitDpos(native *native.NativeService) ([]byte, error) {
	if native.ShardID.ParentID() == native.ShardID {
		return utils.BYTE_FALSE, fmt.Errorf("ShardCommitDpos: only can be invoked at child shard")
	}
	rootShard := common.RootShardID
	if !native.ContextRef.CheckCallShard(rootShard) {
		return utils.BYTE_FALSE, fmt.Errorf("ShardCommitDpos: only can be invoked by ShardCall")
	}
	contract := native.ContextRef.CurrentContext().ContractAddress
	balance, err := ong.GetOngBalance(native, contract)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardCommitDpos: get shard fee balance failed, err: %s", err)
	}
	if ont.AppCallTransfer(native, utils.OngContractAddress, contract, utils.ShardAssetAddress, balance) != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardCommitDpos: transfer ong failed, err: %s", err)
	}
	balanceParam := common.BigIntToNeoBytes(new(big.Int).SetUint64(balance))
	transferIdBytes, err := native.NativeCall(utils.ShardAssetAddress, oep4.COMMIT_DPOS, balanceParam)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardCommitDpos: xshard transfer failed, err: %s", err)
	}
	transferId := common.BigIntFromNeoBytes(transferIdBytes.([]byte))
	xshardHandlingFee, err := getXShardHandlingFee(native)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardCommitDpos: xshard transfer failed, err: %s", err)
	}
	shardStakeCommitParam := &shard_stake.CommitDposParam{
		ShardId:   native.ShardID,
		FeeAmount: balance,
		Height:    native.Height,
		Hash:      native.Tx.Hash(),
		Debt:      xshardHandlingFee.Debt,
		Income:    xshardHandlingFee.Income,
	}
	sink := common.NewZeroCopySink(0)
	shardStakeCommitParam.Serialization(sink)
	native.NotifyRemoteShard(rootShard, utils.ShardStakeAddress, native.ContextRef.GetRemainGas(),
		shard_stake.COMMIT_DPOS, sink.Bytes())
	info := &shardstates.ShardCommitDposInfo{
		TransferId:          transferId,
		FeeAmount:           balance,
		Height:              native.Height,
		Hash:                native.Tx.Hash(),
		XShardHandleFeeInfo: &shard_stake.XShardFeeInfo{Debt: xshardHandlingFee.Debt, Income: xshardHandlingFee.Income}}
	setShardCommitDposInfo(native, info)
	return utils.BYTE_TRUE, nil
}

func ShardRetryCommitDpos(native *native.NativeService) ([]byte, error) {
	if native.ShardID.ParentID() == native.ShardID {
		return utils.BYTE_FALSE, fmt.Errorf("ShardRetryCommitDpos: only can be invoked at shard")
	}
	info, err := getShardCommitDposInfo(native)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardRetryCommitDpos: failed, err: %s", err)
	}
	retryParam := common.BigIntToNeoBytes(info.TransferId)
	if _, err := native.NativeCall(utils.ShardAssetAddress, oep4.RETRY_COMMIT_DPOS, retryParam); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("ShardRetryCommitDpos: xshard transfer retry failed, err: %s", err)
	}
	shardStakeCommitParam := &shard_stake.CommitDposParam{
		ShardId:   native.ShardID,
		FeeAmount: info.FeeAmount,
		Hash:      info.Hash,
		Height:    info.Height,
		Debt:      info.XShardHandleFeeInfo.Debt,
		Income:    info.XShardHandleFeeInfo.Income,
	}
	sink := common.NewZeroCopySink(0)
	shardStakeCommitParam.Serialization(sink)
	rootShard := common.RootShardID
	native.NotifyRemoteShard(rootShard, utils.ShardStakeAddress, native.ContextRef.GetRemainGas(),
		shard_stake.COMMIT_DPOS, sink.Bytes())
	return utils.BYTE_TRUE, nil
}

// only can be invoke while shard call
func UpdateXShardHandlingFee(native *native.NativeService) ([]byte, error) {
	if !native.ContextRef.CheckCallShard(native.ShardID) {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateXShardHandlingFee: check call shard failed")
	}
	param := &XShardHandlingFeeParam{}
	if err := param.Deserialization(common.NewZeroCopySource(native.Input)); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateXShardHandlingFee: failed, err: %s", err)
	}
	shardViewIndex, err := shard_stake.GetShardCurrentViewIndex(native, param.ShardId)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateXShardHandlingFee: failed, err: %s", err)
	}
	if err := updateXShardHandlingFee(native, param, shardViewIndex); err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("UpdateXShardHandlingFee: failed, err: %s", err)
	}
	return utils.BYTE_TRUE, nil
}

func GetShardCommitDPosInfo(native *native.NativeService) ([]byte, error) {
	if native.ShardID.ParentID() == native.ShardID {
		return utils.BYTE_FALSE, fmt.Errorf("GetShardCommitDPosInfo: only can be invoked at shard")
	}
	info, err := getShardCommitDposInfo(native)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("GetShardCommitDPosInfo: failed, err: %s", err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("GetShardCommitDPosInfo: marshal info failed, err: %s", err)
	}
	return []byte(data), nil
}

func GetShardDetail(native *native.NativeService) ([]byte, error) {
	if !native.ShardID.IsRootShard() {
		return utils.BYTE_FALSE, fmt.Errorf("GetShardDetail: only can be invoked at root")
	}
	shardId, err := utils.ReadVarUint(bytes.NewReader(native.Input))
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("GetShardDetail: read param failed, err: %s", err)
	}
	shard, err := common.NewShardID(shardId)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("GetShardDetail: invalid shardId %d", shard)
	}
	contract := native.ContextRef.CurrentContext().ContractAddress
	state, err := GetShardState(native, contract, shard)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("GetShardDetail: read db failed, err: %s", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return utils.BYTE_FALSE, fmt.Errorf("GetShardDetail: marshal detail failed, err: %s", err)
	}
	return data, nil
}
