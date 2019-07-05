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

package TestXShard

import (
	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/config"
	"github.com/ontio/ontology/core/ledger"
	"github.com/ontio/ontology/testsuite"
	TestCommon "github.com/ontio/ontology/testsuite/common"
	TestConsensus "github.com/ontio/ontology/testsuite/consensus"
	TestContracts "github.com/ontio/ontology/testsuite/smartcontract/sys-contract"
	"github.com/ontio/ontology/testsuite/utils"
	"testing"
)

func init() {
	TestConsts.TestRootDir = "../"
}

func TestBlockXShardInfo(t *testing.T) {

}

func TestSoloCommitDpos(t *testing.T) {
	utils.ClearTestChain(t)
	TestContracts.StartShard(t)
	rootShardId := common.NewShardIDUnchecked(config.DEFAULT_SHARD_ID)
	rootLedger := ledger.GetShardLedger(rootShardId)
	soloBookkeeperName := TestCommon.GetOwnerName(rootShardId, 0)
	TestConsensus.StartMokerSoloConsensus(t, rootShardId, soloBookkeeperName, rootLedger)
}
