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

package common

// DataEntryPrefix
type DataEntryPrefix byte

const (
	// DATA
	DATA_BLOCK             DataEntryPrefix = 0x00 //Block height => block hash key prefix
	DATA_HEADER                            = 0x01 //Block hash => block hash key prefix
	DATA_TRANSACTION                       = 0x02 //Transction hash = > transaction key prefix
	DATA_STATE_MERKLE_ROOT                 = 0x21 // block height => write set hash + state merkle root

	// Transaction
	ST_BOOKKEEPER DataEntryPrefix = 0x03 //BookKeeper state key prefix
	ST_CONTRACT   DataEntryPrefix = 0x04 //Smart contract state key prefix
	ST_STORAGE    DataEntryPrefix = 0x05 //Smart contract storage key prefix
	ST_VALIDATOR  DataEntryPrefix = 0x07 //no use
	ST_VOTE       DataEntryPrefix = 0x08 //Vote state key prefix

	IX_HEADER_HASH_LIST DataEntryPrefix = 0x09 //Block height => block hash key prefix

	ST_CONTRACT_META_DATA DataEntryPrefix = 0x0a // contract meta data

	//SYSTEM
	SYS_CURRENT_BLOCK      DataEntryPrefix = 0x10 //Current block key prefix
	SYS_VERSION            DataEntryPrefix = 0x11 //Store version key prefix
	SYS_CURRENT_STATE_ROOT DataEntryPrefix = 0x12 //no use
	SYS_BLOCK_MERKLE_TREE  DataEntryPrefix = 0x13 // Block merkle tree root key prefix
	SYS_STATE_MERKLE_TREE  DataEntryPrefix = 0x20 // state merkle tree root key prefix

	EVENT_NOTIFY DataEntryPrefix = 0x14 //Event notify key prefix

	SHARD_EVENTS DataEntryPrefix = 0x32 // block height -> shard events

	XSHARD_STATE               DataEntryPrefix = 0x34
	XSHARD_KEY_SHARDS_IN_BLOCK                 = 0x35 // with block#, contains to-shard list
	XSHARD_KEY_REQS_IN_BLOCK                   = 0x36 // with block# - shard#, containers requests to shard#
	XSHARD_KEY_MSG_HASH        DataEntryPrefix = 0x37 //shard msg key
	XSHARD_KEY_LOCKED_ADDRESS                  = 0x38 // save current locked contrqct address
	XSHARD_KEY_LOCKED_KEY      DataEntryPrefix = 0x39

	CROSS_SHARD_MSG    DataEntryPrefix = 0x40 //cross shard msg data
	CROSS_SHARD_HEIGHT DataEntryPrefix = 0x41 //all shard consensus height info

	SHARD_CONFIG_DATA DataEntryPrefix = 0x42 //all shard consensus config

	CROSS_ALL_SHARDS                 DataEntryPrefix = 0x43 //cross shard all shardId
	CROSS_SHARD_HASH                 DataEntryPrefix = 0x44 //cross shard msg hash
	CROSS_SHARD_CONTRACT_META        DataEntryPrefix = 0x45 //cross shard contract meta
	CROSS_SHARD_CONTRACT_META_HEIGHT DataEntryPrefix = 0x46 //all cross shard contract meta height info
	CROSS_SHARD_CONTRACT_EVENT       DataEntryPrefix = 0x47 //cross shard contract event
	DATA_SHARD_TX                                    = 0x48 //shardTx hash = > shardTx key prefix
	DATA_SHARD_TX_HASHES                             = 0x49 //shardTx hashes = > shardTx hashes key prefix
	DATA_SOURCE_TX_HASH                              = 0x50 // sourceTx hash = > shardTx hash
)
