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

package TestCommon

import (
	"testing"

	"github.com/ontio/ontology-crypto/keypair"
	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/config"
	"github.com/ontio/ontology/core/chainmgr"
	"github.com/ontio/ontology/core/genesis"
	"github.com/ontio/ontology/core/ledger"
	"github.com/ontio/ontology/testsuite"
)

func CreateChain(t *testing.T, name string, shardID common.ShardID, genesisParentHeight uint32) {
	if lgr := ledger.GetShardLedger(shardID); lgr != nil {
		return
	}

	cfg := GetConfig(t, shardID)
	if cfg == nil {
		t.Fatalf("nil config for shard %d", shardID)
	}

	dataDir := TestConsts.TestRootDir + "Chain/" + name + "/"

	var lgr *ledger.Ledger
	var err error
	if shardID.IsRootShard() {
		lgr, err = ledger.NewLedger(dataDir, 0)
	} else {
		rootLgr := ledger.GetShardLedger(shardID.ParentID())
		lgr, err = ledger.NewShardLedger(shardID, dataDir, rootLgr)
	}

	if err != nil {
		t.Fatalf("failed to init ledger %d: %s", shardID, err)
	}

	bookkeeper := GetAccount(chainmgr.GetShardName(shardID) + "_peerOwner0")
	if bookkeeper == nil {
		t.Fatalf("failed to get shard %d, user peer owner0", shardID)
	}
	shardCfg := &config.ShardConfig{
		ShardID:             shardID,
		GenesisParentHeight: genesisParentHeight,
	}
	bookkeepers := []keypair.PublicKey{bookkeeper.PublicKey}
	blk, err := genesis.BuildGenesisBlock(bookkeepers, cfg.Genesis, shardCfg)
	if err != nil {
		t.Fatalf("build genesis block %d, err: %s", shardID, err)
	}

	if err := lgr.Init(bookkeepers, blk); err != nil {
		t.Fatalf("init ledger %d failed: %s", shardID, err)
	}
}

func GetHeight(t *testing.T, shardID common.ShardID) uint32 {
	if lgr := ledger.GetShardLedger(shardID); lgr != nil {
		return lgr.GetCurrentBlockHeight()
	}
	t.Fatalf("get height with invalid shard %d", shardID)
	return 0
}

func CloneChain(t *testing.T, name string, srcLgr *ledger.Ledger) *ledger.Ledger {
	dataDir := TestConsts.TestRootDir + "Chain/" + name + "/"
	shardID := srcLgr.ShardID

	var lgr *ledger.Ledger
	var err error
	if shardID.IsRootShard() {
		lgr, err = ledger.NewLedger(dataDir, 0)
	} else {
		rootLgr := ledger.GetShardLedger(shardID.ParentID())
		lgr, err = ledger.NewShardLedger(shardID, dataDir, rootLgr)
	}

	blk, err := srcLgr.GetBlockByHeight(0)
	if err != nil {
		t.Fatalf("chain %s: failed get geneis block of source ledger", name)
	}

	if err := lgr.Init(blk.Header.Bookkeepers, blk); err != nil {
		t.Fatalf("init ledger %d failed: %s", shardID, err)
	}

	return lgr
}
