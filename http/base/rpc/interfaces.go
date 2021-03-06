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

package rpc

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/config"
	"github.com/ontio/ontology/common/log"
	"github.com/ontio/ontology/core/chainmgr"
	"github.com/ontio/ontology/core/payload"
	scom "github.com/ontio/ontology/core/store/common"
	"github.com/ontio/ontology/core/types"
	ontErrors "github.com/ontio/ontology/errors"
	bactor "github.com/ontio/ontology/http/base/actor"
	bcomn "github.com/ontio/ontology/http/base/common"
	berr "github.com/ontio/ontology/http/base/error"
	"github.com/ontio/ontology/smartcontract/service/native/utils"
)

//get best block hash
func GetBestBlockHash(params []interface{}) map[string]interface{} {
	hash := bactor.CurrentBlockHash()
	return responseSuccess(hash.ToHexString())
}

// get block by height or hash
// Input JSON string examples for getblock method as following:
//   {"jsonrpc": "2.0", "method": "getblock", "params": [1], "id": 0}
//   {"jsonrpc": "2.0", "method": "getblock", "params": ["aabbcc.."], "id": 0}
func GetBlock(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}
	var err error
	var hash common.Uint256
	switch (params[0]).(type) {
	// block height
	case float64:
		index := uint32(params[0].(float64))
		hash = bactor.GetBlockHashFromStore(index)
		if hash == common.UINT256_EMPTY {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		// block hash
	case string:
		str := params[0].(string)
		hash, err = common.Uint256FromHexString(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}
	block, err := bactor.GetBlockFromStore(hash)
	if err != nil {
		return responsePack(berr.UNKNOWN_BLOCK, "unknown block")
	}
	if len(params) >= 2 {
		switch (params[1]).(type) {
		case float64:
			json := uint32(params[1].(float64))
			if json == 1 {
				return responseSuccess(bcomn.GetBlockInfo(block))
			}
		default:
			return responsePack(berr.INVALID_PARAMS, "")
		}
	}
	return responseSuccess(common.ToHexString(block.ToArray()))
}

//get block height
func GetBlockCount(params []interface{}) map[string]interface{} {
	height := bactor.GetCurrentBlockHeight()
	return responseSuccess(height + 1)
}

//get block hash
// A JSON example for getblockhash method as following:
//   {"jsonrpc": "2.0", "method": "getblockhash", "params": [1], "id": 0}
func GetBlockHash(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}
	switch params[0].(type) {
	case float64:
		height := uint32(params[0].(float64))
		hash := bactor.GetBlockHashFromStore(height)
		if hash == common.UINT256_EMPTY {
			return responsePack(berr.UNKNOWN_BLOCK, "")
		}
		return responseSuccess(hash.ToHexString())
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}
}

//get node connection count
func GetConnectionCount(params []interface{}) map[string]interface{} {
	count, err := bactor.GetConnectionCnt()
	if err != nil {
		log.Errorf("GetConnectionCount error:%s", err)
		return responsePack(berr.INTERNAL_ERROR, false)
	}
	return responseSuccess(count)
}

//get memory pool transaction count
func GetMemPoolTxCount(params []interface{}) map[string]interface{} {
	count, err := bactor.GetTxnCount()
	if err != nil {
		return responsePack(berr.INTERNAL_ERROR, nil)
	}
	return responseSuccess(count)
}

//get memory pool transaction state
func GetMemPoolTxState(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}
	switch params[0].(type) {
	case string:
		str := params[0].(string)
		hash, err := common.Uint256FromHexString(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		txEntry, err := bactor.GetTxFromPool(hash)
		if err != nil {
			return responsePack(berr.UNKNOWN_TRANSACTION, "unknown transaction")
		}
		attrs := []bcomn.TXNAttrInfo{}
		for _, t := range txEntry.Attrs {
			attrs = append(attrs, bcomn.TXNAttrInfo{t.Height, int(t.Type), int(t.ErrCode)})
		}
		info := bcomn.TXNEntryInfo{attrs}
		return responseSuccess(info)
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}
}

// get raw transaction in raw or json
// A JSON example for getrawtransaction method as following:
//   {"jsonrpc": "2.0", "method": "getrawtransaction", "params": ["transactioin hash in hex"], "id": 0}
func GetRawTransaction(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}
	var tx *types.Transaction
	var height uint32
	switch params[0].(type) {
	case string:
		str := params[0].(string)
		hash, err := common.Uint256FromHexString(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		h, t, err := bactor.GetTxnWithHeightByTxHash(hash)
		if err != nil {
			return responsePack(berr.UNKNOWN_TRANSACTION, "unknown transaction")
		}
		height = h
		tx = t
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}

	if len(params) >= 2 {
		switch (params[1]).(type) {
		case float64:
			json := uint32(params[1].(float64))
			if json == 1 {
				txinfo := bcomn.TransArryByteToHexString(tx)
				txinfo.Height = height
				return responseSuccess(txinfo)
			}
		default:
			return responsePack(berr.INVALID_PARAMS, "")
		}
	}
	w := bytes.NewBuffer(nil)
	tx.Serialize(w)
	return responseSuccess(common.ToHexString(w.Bytes()))
}

//get storage from contract
//   {"jsonrpc": "2.0", "method": "getstorage", "params": ["code hash", "key"], "id": 0}
func GetStorage(params []interface{}) map[string]interface{} {
	if len(params) < 2 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}

	var address common.Address
	var key []byte
	switch params[0].(type) {
	case string:
		str := params[0].(string)
		var err error
		address, err = bcomn.GetAddress(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}

	switch params[1].(type) {
	case string:
		str := params[1].(string)
		hex, err := hex.DecodeString(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		key = hex
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}
	value, err := bactor.GetStorageItem(address, key)
	if err != nil {
		if err == scom.ErrNotFound {
			return responseSuccess(nil)
		}
		return responsePack(berr.INVALID_PARAMS, "")
	}
	return responseSuccess(common.ToHexString(value))
}

//get storage from contract
//   {"jsonrpc": "2.0", "method": "getstorage", "params": ["shardID", "code hash", "key"], "id": 0}
func GetShardStorage(params []interface{}) map[string]interface{} {
	if len(params) < 3 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}

	var shardID uint64
	var address common.Address
	var key []byte
	var err error
	switch params[0].(type) {
	case string:
		str := params[0].(string)
		shardID, err = strconv.ParseUint(str, 10, 64)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, err.Error())
		}
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}

	switch params[1].(type) {
	case string:
		str := params[1].(string)
		address, err = bcomn.GetAddress(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}

	switch params[2].(type) {
	case string:
		str := params[2].(string)
		hex, err := hex.DecodeString(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		key = hex
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}

	log.Errorf(">>>> recevied shard storage get: %d,chainmgr:%d, %s", shardID, chainmgr.GetShardID().ToUint64(), key)

	var value []byte
	if shardID == chainmgr.GetShardID().ToUint64() {
		value, err = bactor.GetStorageItem(address, key)
	} else {
		err = fmt.Errorf("param shardId:%d,GetShardID:%d  unmatch", shardID, chainmgr.GetShardID().ToUint64())
		log.Error(err)
	}
	if err != nil {
		if err == scom.ErrNotFound {
			return responseSuccess(nil)
		}
		return responsePack(berr.INVALID_PARAMS, err.Error())
	}
	return responseSuccess(common.ToHexString(value))
}

//get storage from contract
//   {"jsonrpc": "2.0", "method": "getshardtxstate", "params": ["tx hash", "notify id"], "id": 0}
func GetShardTxState(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}

	str, ok := params[0].(string)
	if !ok {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	txHash, err := common.Uint256FromHexString(str)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, err.Error())
	}
	var notifyId uint64
	var isHasNotify bool
	if len(params) > 1 {
		str, ok := params[1].(string)
		if !ok {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		notifyId, err = strconv.ParseUint(str, 10, 64)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		isHasNotify = true
	}
	value, err := bactor.GetShardTxState(txHash, uint32(notifyId), isHasNotify)
	if err != nil {
		if err == scom.ErrNotFound {
			return responseSuccess(nil)
		}
		return responsePack(berr.INVALID_PARAMS, err.Error())
	}
	r, err := bcomn.ParseShardState(value)
	if err != nil {
		return responsePack(berr.INTERNAL_ERROR, err.Error())
	}
	return responseSuccess(r)
}

//send raw transaction
// A JSON example for sendrawtransaction method as following:
//   {"jsonrpc": "2.0", "method": "sendrawtransaction", "params": ["raw transactioin in hex"], "id": 0}
func SendRawTransaction(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}
	var hash common.Uint256
	switch params[0].(type) {
	case string:
		str := params[0].(string)
		raw, err := common.HexToBytes(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		txn, err := types.TransactionFromRawBytes(raw)
		if err != nil {
			return responsePack(berr.INVALID_TRANSACTION, "")
		}
		hash = txn.Hash()
		log.Debugf("SendRawTransaction recv %s", hash.ToHexString())
		if txn.TxType == types.Invoke || txn.TxType == types.Deploy {
			if len(params) > 1 {
				preExec, ok := params[1].(float64)
				if ok && preExec == 1 {
					result, err := bactor.PreExecuteContract(txn)
					if err != nil {
						log.Infof("PreExec: ", err)
						return responsePack(berr.SMARTCODE_ERROR, err.Error())
					}
					return responseSuccess(bcomn.ConvertPreExecuteResult(result))
				}
			}
		}

		log.Debugf("SendRawTransaction send to %d, %d txpool %s", txn.ShardID, chainmgr.GetShardID(), hash.ToHexString())
		if txn.ShardID == chainmgr.GetShardID() {
			if errCode, desc := bcomn.SendTxToPool(txn); errCode != ontErrors.ErrNoError {
				log.Warnf("SendRawTransaction verified %s error: %s", hash.ToHexString(), desc)
				return responsePack(int64(errCode), desc)
			}
		} else if txn.ShardID == common.NewShardIDUnchecked(config.DEFAULT_SHARD_ID) {
			return responsePack(int64(ontErrors.ErrXmitFail), "")
		} else {
			return responsePack(int64(ontErrors.ErrInValidShard), "")
		}
		log.Debugf("SendRawTransaction verified %s", hash.ToHexString())
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}
	return responseSuccess(hash.ToHexString())
}

//get node version
func GetNodeVersion(params []interface{}) map[string]interface{} {
	return responseSuccess(config.Version)
}

// get networkid
func GetNetworkId(params []interface{}) map[string]interface{} {
	return responseSuccess(config.DefConfig.P2PNode.NetworkId)
}

//get contract state
func GetContractState(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}
	var contract *payload.DeployCode
	switch params[0].(type) {
	case string:
		str := params[0].(string)
		address, err := bcomn.GetAddress(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		c, err := bactor.GetContractStateFromStore(address)
		if err != nil {
			return responsePack(berr.UNKNOWN_CONTRACT, "unknow contract")
		}
		contract = c
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}
	if len(params) >= 2 {
		switch (params[1]).(type) {
		case float64:
			json := uint32(params[1].(float64))
			if json == 1 {
				return responseSuccess(bcomn.TransPayloadToHex(contract))
			}
		default:
			return responsePack(berr.INVALID_PARAMS, "")
		}
	}
	w := bytes.NewBuffer(nil)
	contract.Serialize(w)
	return responseSuccess(common.ToHexString(w.Bytes()))
}

func GetShardTxHash(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}
	switch params[0].(type) {
	case string:
		str := params[0].(string)
		hash, err := common.Uint256FromHexString(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		shardTxHash, err := bactor.GetShardTxHashBySourceTxHash(hash)
		if err != nil {
			if err == scom.ErrNotFound {
				return responseSuccess(nil)
			}
			return responsePack(berr.INTERNAL_ERROR, err)
		}
		return responseSuccess(shardTxHash)
	default:
		return responsePack(berr.INVALID_PARAMS, nil)
	}
}

//get smartconstract event by txhash, sourcetxhash
func GetSmartCodeEvent(params []interface{}) map[string]interface{} {
	if !config.DefConfig.Common.EnableEventLog {
		return responsePack(berr.INVALID_METHOD, "")
	}
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}

	switch (params[0]).(type) {
	// block height
	case float64:
		height := uint32(params[0].(float64))
		eventInfos, err := bactor.GetEventNotifyByHeight(height)
		if err != nil {
			if err == scom.ErrNotFound {
				return responseSuccess(nil)
			}
			return responsePack(berr.INTERNAL_ERROR, "")
		}
		eInfos := make([]*bcomn.ExecuteNotify, 0, len(eventInfos))
		for _, eventInfo := range eventInfos {
			_, notify := bcomn.GetExecuteNotify(eventInfo)
			eInfos = append(eInfos, &notify)
		}
		return responseSuccess(eInfos)
		//txhash
	case string:
		str := params[0].(string)
		hash, err := common.Uint256FromHexString(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		eventInfo, err := bactor.GetEventNotifyByTxHash(hash)
		if err != nil {
			if scom.ErrNotFound == err {
				return responseSuccess(nil)
			}
			return responsePack(berr.INTERNAL_ERROR, "")
		}
		_, notify := bcomn.GetExecuteNotify(eventInfo)
		return responseSuccess(notify)
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}
	return responsePack(berr.INVALID_PARAMS, "")
}

//get shard smartcontract event by sourcetxhash
func GetShardSmartCodeEvent(params []interface{}) map[string]interface{} {
	if !config.DefConfig.Common.EnableEventLog {
		return responsePack(berr.INVALID_METHOD, "")
	}
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}

	switch (params[0]).(type) {
	//txhash
	case string:
		sourceTxHashStr := params[0].(string)
		sourceTxHash, err := common.Uint256FromHexString(sourceTxHashStr)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		shardTxHash, err := bactor.GetShardTxHashBySourceTxHash(sourceTxHash)
		if err != nil {
			if scom.ErrNotFound == err {
				return responseSuccess(nil)
			}
			return responsePack(berr.INTERNAL_ERROR, err)
		}
		eventInfo, err := bactor.GetEventNotifyByTxHash(shardTxHash)
		if err != nil {
			if scom.ErrNotFound == err {
				return responseSuccess(nil)
			}
			return responsePack(berr.INTERNAL_ERROR, "")
		}
		_, notify := bcomn.GetExecuteNotify(eventInfo)
		info := make([]bcomn.NotifyEventInfo, 0)
		for _, n := range notify.Notify {
			if n.SourceTxHash == sourceTxHashStr {
				info = append(info, n)
			}
		}
		notify.Notify = info
		return responseSuccess(notify)
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}
	return responsePack(berr.INVALID_PARAMS, "")
}

//get block height by transaction hash
func GetBlockHeightByTxHash(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}

	switch (params[0]).(type) {
	// tx hash
	case string:
		str := params[0].(string)
		hash, err := common.Uint256FromHexString(str)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		height, _, err := bactor.GetTxnWithHeightByTxHash(hash)
		if err != nil {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		return responseSuccess(height)
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}
	return responsePack(berr.INVALID_PARAMS, "")
}

//get balance of address
func GetBalance(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	strAddr, ok := params[0].(string)
	if !ok {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	var address common.Address
	var err error = nil
	if len(strAddr) == 40 {
		address, err = common.AddressFromHexString(strAddr)
	} else {
		address, err = common.AddressFromBase58(strAddr)
	}
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	rsp, err := bcomn.GetBalance(address)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	return responseSuccess(rsp)
}

//get allowance
func GetAllowance(params []interface{}) map[string]interface{} {
	if len(params) < 3 {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	asset, ok := params[0].(string)
	if !ok {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	fromAddrStr, ok := params[1].(string)
	if !ok {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	fromAddr, err := bcomn.GetAddress(fromAddrStr)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	toAddrStr, ok := params[2].(string)
	if !ok {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	toAddr, err := bcomn.GetAddress(toAddrStr)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	rsp, err := bcomn.GetAllowance(asset, fromAddr, toAddr)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	return responseSuccess(rsp)
}

//get merkle proof by transaction hash
func GetMerkleProof(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	str, ok := params[0].(string)
	if !ok {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	hash, err := common.Uint256FromHexString(str)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	height, _, err := bactor.GetTxnWithHeightByTxHash(hash)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	header, err := bactor.GetHeaderByHeight(height)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}

	curHeight := bactor.GetCurrentBlockHeight()
	curHeader, err := bactor.GetHeaderByHeight(curHeight)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	proof, err := bactor.GetMerkleProof(uint32(height), uint32(curHeight))
	if err != nil {
		return responsePack(berr.INTERNAL_ERROR, "")
	}
	var hashes []string
	for _, v := range proof {
		hashes = append(hashes, v.ToHexString())
	}
	return responseSuccess(bcomn.MerkleProof{"MerkleProof", header.TransactionsRoot.ToHexString(), height,
		curHeader.BlockRoot.ToHexString(), curHeight, hashes})
}

//get block transactions by height
func GetBlockTxsByHeight(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, nil)
	}
	switch params[0].(type) {
	case float64:
		height := uint32(params[0].(float64))
		hash := bactor.GetBlockHashFromStore(height)
		if hash == common.UINT256_EMPTY {
			return responsePack(berr.INVALID_PARAMS, "")
		}
		block, err := bactor.GetBlockFromStore(hash)
		if err != nil {
			return responsePack(berr.UNKNOWN_BLOCK, "")
		}
		return responseSuccess(bcomn.GetBlockTransactions(block))
	default:
		return responsePack(berr.INVALID_PARAMS, "")
	}
}

//get gas price in block
func GetGasPrice(params []interface{}) map[string]interface{} {
	result, err := bcomn.GetGasPrice()
	if err != nil {
		return responsePack(berr.INTERNAL_ERROR, "")
	}
	return responseSuccess(result)
}

// get unbound ong of address
func GetUnboundOng(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	str, ok := params[0].(string)
	if !ok {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	toAddr, err := common.AddressFromBase58(str)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	fromAddr := utils.OntContractAddress
	rsp, err := bcomn.GetAllowance("ong", fromAddr, toAddr)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	return responseSuccess(rsp)
}

// get grant ong of address
func GetGrantOng(params []interface{}) map[string]interface{} {
	if len(params) < 1 {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	str, ok := params[0].(string)
	if !ok {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	toAddr, err := common.AddressFromBase58(str)
	if err != nil {
		return responsePack(berr.INVALID_PARAMS, "")
	}
	rsp, err := bcomn.GetGrantOng(toAddr)
	if err != nil {
		return responsePack(berr.INTERNAL_ERROR, "")
	}
	return responseSuccess(rsp)
}
