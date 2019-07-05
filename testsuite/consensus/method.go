package TestConsensus

import (
	"fmt"
	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/config"
	"github.com/ontio/ontology/consensus"
	"github.com/ontio/ontology/core/chainmgr"
	"github.com/ontio/ontology/core/ledger"
	TestCommon "github.com/ontio/ontology/testsuite/common"
	"testing"
	"time"
)

func StartMockerConsensus(t *testing.T, shardID common.ShardID, name string, srcLgr *ledger.Ledger) consensus.ConsensusService {
	shardName := chainmgr.GetShardName(shardID)

	acc := TestCommon.GetAccount(shardName + "_" + name)
	if acc == nil {
		t.Fatalf("failed to get user account of shard %s", shardName)
	}

	lgr := TestCommon.CloneChain(t, name, srcLgr)
	ledger.RemoveLedger(shardID)

	txPool := TestCommon.NewTxnPool(t, name, shardID)
	peer := TestCommon.NewPeer(lgr)
	peer.Register()
	p2pActor := TestCommon.NewP2PActor(t, name, peer)

	txPool.Start(t)
	p2pActor.Start(t)
	peer.Start()

	service, err := consensus.NewConsensusService(consensus.CONSENSUS_VBFT, shardID, acc, txPool.GetPID(t), lgr, p2pActor.GetPID(t))
	if err != nil {
		t.Fatalf("start consensus: %s", err)
	}
	peer.SetConsensusPid(t, service.GetPID())
	return service
}

func StartMokerSoloConsensus(t *testing.T, shardID common.ShardID, userName string,
	srcLgr *ledger.Ledger) (consensus.ConsensusService, *ledger.Ledger) {
	acc := TestCommon.GetAccount(userName)
	if acc == nil {
		t.Fatalf("failed to get user account %s", userName)
	}

	lgr := TestCommon.CloneChain(t, userName, srcLgr)
	ledger.RemoveLedger(shardID)

	txPool := TestCommon.NewTxnPool(t, fmt.Sprintf("%s%d", userName, time.Now().Unix()), shardID)
	peer := TestCommon.NewPeer(lgr)
	peer.Register()
	p2pActor := TestCommon.NewP2PActor(t, userName, peer)

	txPool.Start(t)
	p2pActor.Start(t)
	peer.Start()

	config.DefConfig.Genesis.SOLO.GenBlockTime = 1
	service, err := consensus.NewConsensusService(consensus.CONSENSUS_SOLO, shardID, acc, txPool.GetPID(t), lgr, p2pActor.GetPID(t))
	if err != nil {
		t.Fatalf("start consensus: %s", err)
	}
	peer.SetConsensusPid(t, service.GetPID())
	return service, lgr
}
