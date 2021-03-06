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

package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	_ "net/http/pprof"

	"github.com/ethereum/go-ethereum/common/fdlimit"
	"github.com/ontio/ontology-crypto/keypair"
	"github.com/ontio/ontology-eventbus/actor"
	alog "github.com/ontio/ontology-eventbus/log"
	"github.com/ontio/ontology/account"
	"github.com/ontio/ontology/cmd"
	cmdcom "github.com/ontio/ontology/cmd/common"
	"github.com/ontio/ontology/cmd/utils"
	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/config"
	"github.com/ontio/ontology/common/log"
	"github.com/ontio/ontology/core/chainmgr"
	"github.com/ontio/ontology/core/ledger"
	"github.com/ontio/ontology/events"
	bactor "github.com/ontio/ontology/http/base/actor"
	hserver "github.com/ontio/ontology/http/base/actor"
	"github.com/ontio/ontology/http/jsonrpc"
	"github.com/ontio/ontology/http/localrpc"
	"github.com/ontio/ontology/http/nodeinfo"
	"github.com/ontio/ontology/http/restful"
	"github.com/ontio/ontology/http/websocket"
	"github.com/ontio/ontology/p2pserver"
	netreqactor "github.com/ontio/ontology/p2pserver/actor/req"
	p2pactor "github.com/ontio/ontology/p2pserver/actor/server"
	"github.com/ontio/ontology/txnpool"
	tc "github.com/ontio/ontology/txnpool/common"
	"github.com/ontio/ontology/validator/stateful"
	"github.com/ontio/ontology/validator/stateless"
	"github.com/urfave/cli"
)

func setupAPP() *cli.App {
	app := cli.NewApp()
	app.Usage = "Ontology CLI"
	app.Action = startOntology
	app.Version = config.Version
	app.Copyright = "Copyright in 2018 The Ontology Authors"
	app.Commands = []cli.Command{
		cmd.AccountCommand,
		cmd.InfoCommand,
		cmd.AssetCommand,
		cmd.ContractCommand,
		cmd.ImportCommand,
		cmd.ExportCommand,
		cmd.TxCommond,
		cmd.SigTxCommand,
		cmd.MultiSigAddrCommand,
		cmd.MultiSigTxCommand,
		cmd.SendTxCommand,
		cmd.ShowTxCommand,
	}
	app.Flags = []cli.Flag{
		//common setting
		utils.ConfigFlag,
		utils.LogLevelFlag,
		utils.DisableEventLogFlag,
		utils.DataDirFlag,
		//account setting
		utils.WalletFileFlag,
		utils.AccountAddressFlag,
		utils.AccountPassFlag,
		//consensus setting
		utils.EnableConsensusFlag,
		utils.MaxTxInBlockFlag,
		//txpool setting
		utils.GasPriceFlag,
		utils.GasLimitFlag,
		utils.TxpoolPreExecDisableFlag,
		utils.DisableSyncVerifyTxFlag,
		utils.DisableBroadcastNetTxFlag,
		//p2p setting
		utils.ReservedPeersOnlyFlag,
		utils.ReservedPeersFileFlag,
		utils.NetworkIdFlag,
		utils.NodePortFlag,
		utils.HttpInfoPortFlag,
		utils.MaxConnInBoundFlag,
		utils.MaxConnOutBoundFlag,
		utils.MaxConnInBoundForSingleIPFlag,
		//test mode setting
		utils.EnableTestModeFlag,
		utils.TestModeGenBlockTimeFlag,
		//rpc setting
		utils.RPCDisabledFlag,
		utils.RPCPortFlag,
		utils.RPCLocalEnableFlag,
		utils.RPCLocalProtFlag,
		//rest setting
		utils.RestfulEnableFlag,
		utils.RestfulPortFlag,
		utils.RestfulMaxConnsFlag,
		//ws setting
		utils.WsEnabledFlag,
		utils.WsPortFlag,
		//sharding setting
		utils.ShardIDFlag,
		utils.EnableSoloShardFlag,
		utils.ShardParentHeightFlag,
	}
	app.Before = func(context *cli.Context) error {
		runtime.GOMAXPROCS(runtime.NumCPU())
		return nil
	}
	return app
}

func main() {
	if err := setupAPP().Run(os.Args); err != nil {
		cmd.PrintErrorMsg(err.Error())
		os.Exit(1)
	}
}

func startOntology(ctx *cli.Context) {
	id := ctx.Uint64(utils.GetFlagName(utils.ShardIDFlag))
	shardID, err := common.NewShardID(id)
	if err != nil {
		fmt.Printf("wrong shard id:%d", id)
	}
	initLog(ctx)

	log.Infof("ontology version %s", config.Version)

	setMaxOpenFiles()
	startMainChain(ctx, shardID)
}

func startMainChain(ctx *cli.Context, shardID common.ShardID) {
	initLog(ctx)

	if _, err := initConfig(ctx); err != nil {
		log.Errorf("initConfig error:%s", err)
		return
	}
	acc, err := initAccount(ctx)
	if err != nil {
		log.Errorf("initWallet error:%s", err)
		return
	}
	if acc != nil {
		pubkey := hex.EncodeToString(keypair.SerializePublicKey(acc.PublicKey))
		log.Infof("server: %s starting", pubkey)
	}

	events.Init() //Init event hub

	// start chain manager
	chainmgr, err := initChainManager(ctx, shardID, acc)
	if err != nil {
		log.Errorf("init main chain manager error: %s", err)
		return
	}
	defer ledger.CloseLedgers()
	defer chainmgr.Close()

	txPoolMgr, err := initTxPool(ctx, shardID, chainmgr)
	if err != nil {
		log.Errorf("initTxPool error:%s", err)
		return
	}
	p2pSvr, _, err := initP2PNode(ctx, shardID, txPoolMgr)
	if err != nil {
		log.Errorf("initP2PNode error:%s", err)
		return
	}

	chainmgr.Start(p2pSvr.GetPID(), txPoolMgr)
	defer chainmgr.Stop()

	err = initRpc(ctx)
	if err != nil {
		log.Errorf("initRpc error:%s", err)
		return
	}
	err = initLocalRpc(ctx)
	if err != nil {
		log.Errorf("initLocalRpc error:%s", err)
		return
	}
	initRestful(ctx)
	initWs(ctx)
	initNodeInfo(ctx, p2pSvr)

	go logCurrBlockHeight(shardID)
	waitToExit()
}

func initLog(ctx *cli.Context) {
	//init log module
	logLevel := ctx.GlobalInt(utils.GetFlagName(utils.LogLevelFlag))
	logPath := log.PATH
	alog.InitLog(logPath)
	log.InitLog(logLevel, logPath, log.Stdout)
}

func initConfig(ctx *cli.Context) (*config.OntologyConfig, error) {
	//init ontology config from cli
	cfg, err := cmd.SetOntologyConfig(ctx)
	if err != nil {
		return nil, err
	}
	log.Infof("Config init success")
	return cfg, nil
}

func initAccount(ctx *cli.Context) (*account.Account, error) {
	if !config.DefConfig.Consensus.EnableConsensus {
		return nil, nil
	}
	walletFile := ctx.GlobalString(utils.GetFlagName(utils.WalletFileFlag))
	if walletFile == "" {
		return nil, fmt.Errorf("Please config wallet file using --wallet flag")
	}
	if !common.FileExisted(walletFile) {
		return nil, fmt.Errorf("Cannot find wallet file:%s. Please create wallet first", walletFile)
	}

	acc, err := cmdcom.GetAccount(ctx)
	if err != nil {
		return nil, fmt.Errorf("get account error:%s", err)
	}
	log.Infof("Using account:%s", acc.Address.ToBase58())

	if config.DefConfig.Genesis.ConsensusType == config.CONSENSUS_TYPE_SOLO {
		curPk := hex.EncodeToString(keypair.SerializePublicKey(acc.PublicKey))
		config.DefConfig.Genesis.SOLO.Bookkeepers = []string{curPk}
	}

	log.Infof("Account init success")
	return acc, nil
}

func initChainManager(ctx *cli.Context, shardID common.ShardID, acc *account.Account) (*chainmgr.ChainManager, error) {
	log.Infof("starting shard %d chain mgr", shardID)

	mgr, err := chainmgr.Initialize(shardID, acc)
	if err != nil {
		return nil, err
	}

	stateHashHeight := config.GetStateHashCheckHeight(config.DefConfig.P2PNode.NetworkId)
	if err := mgr.LoadFromLedger(stateHashHeight); err != nil {
		log.Errorf("load chain mgr from ledger: %s", err)
		return nil, err
	}

	// set Default Ledger
	if lgr := ledger.GetShardLedger(shardID); lgr != nil {
		ledger.DefLedger = lgr
	}

	return mgr, err
}

func initTxPool(ctx *cli.Context, shardID common.ShardID, chainMgr *chainmgr.ChainManager) (*txnpool.TxnPoolManager, error) {
	disablePreExec := ctx.GlobalBool(utils.GetFlagName(utils.TxpoolPreExecDisableFlag))
	bactor.DisableSyncVerifyTx = ctx.GlobalBool(utils.GetFlagName(utils.DisableSyncVerifyTxFlag))
	disableBroadcastNetTx := ctx.GlobalBool(utils.GetFlagName(utils.DisableBroadcastNetTxFlag))

	mgr, err := txnpool.NewTxnPoolManager(shardID, disablePreExec, disableBroadcastNetTx)
	if err != nil {
		return nil, fmt.Errorf("init txPoolMgr failed: %s", err)
	}
	hserver.SetTxPid(mgr.GetPID(shardID, tc.TxActor))

	for _, shardId := range chainMgr.GetActiveShards() {
		lgr := ledger.GetShardLedger(shardId)
		if lgr == nil {
			continue
		}
		srv, err := mgr.StartTxnPoolServer(shardId, lgr)
		if err != nil {
			return nil, fmt.Errorf("Init txpool error:%s", err)
		}
		stlValidator, _ := stateless.NewValidator(fmt.Sprintf("stateless_validator_%d", shardId.ToUint64()))
		stlValidator.Register(srv.GetPID(tc.VerifyRspActor))
		stlValidator2, _ := stateless.NewValidator(fmt.Sprintf("stateless_validator2_%d", shardId.ToUint64()))
		stlValidator2.Register(srv.GetPID(tc.VerifyRspActor))
		stfValidator, _ := stateful.NewValidator(fmt.Sprintf("stateful_validator_%d", shardId.ToUint64()), lgr)
		stfValidator.Register(srv.GetPID(tc.VerifyRspActor))
	}

	log.Infof("TxPool init success")
	return mgr, nil
}

func initP2PNode(ctx *cli.Context, shardID common.ShardID, txpoolMgr *txnpool.TxnPoolManager) (*p2pserver.P2PServer, *actor.PID, error) {
	if config.DefConfig.Genesis.ConsensusType == config.CONSENSUS_TYPE_SOLO && !ctx.Bool(utils.GetFlagName(utils.EnableSoloShardFlag)) {
		return nil, nil, nil
	}

	p2p := p2pserver.NewServer(shardID)

	p2pActor := p2pactor.NewP2PActor(p2p)
	p2pPID, err := p2pActor.Start(shardID)
	if err != nil {
		return nil, nil, fmt.Errorf("p2pActor init error %s", err)
	}
	p2p.SetPID(p2pPID)
	err = p2p.Start()
	if err != nil {
		return nil, nil, fmt.Errorf("p2p service start error %s", err)
	}
	netreqactor.SetTxnPoolPid(txpoolMgr.GetPID(shardID, tc.TxActor))
	txpoolMgr.RegisterActor(tc.NetActor, p2pPID)
	hserver.SetNetServerPID(p2pPID)
	p2p.WaitForPeersStart()
	log.Infof("P2P init success")
	return p2p, p2pPID, nil
}

func initRpc(ctx *cli.Context) error {
	if !config.DefConfig.Rpc.EnableHttpJsonRpc {
		return nil
	}
	var err error
	exitCh := make(chan interface{}, 0)
	go func() {
		err = jsonrpc.StartRPCServer()
		close(exitCh)
	}()

	flag := false
	select {
	case <-exitCh:
		if !flag {
			return err
		}
	case <-time.After(time.Millisecond * 5):
		flag = true
	}
	log.Infof("Rpc init success")
	return nil
}

func initLocalRpc(ctx *cli.Context) error {
	if !ctx.GlobalBool(utils.GetFlagName(utils.RPCLocalEnableFlag)) {
		return nil
	}
	var err error
	exitCh := make(chan interface{}, 0)
	go func() {
		err = localrpc.StartLocalServer()
		close(exitCh)
	}()

	flag := false
	select {
	case <-exitCh:
		if !flag {
			return err
		}
	case <-time.After(time.Millisecond * 5):
		flag = true
	}

	log.Infof("Local rpc init success")
	return nil
}

func initRestful(ctx *cli.Context) {
	if !config.DefConfig.Restful.EnableHttpRestful {
		return
	}
	go restful.StartServer()

	log.Infof("Restful init success")
}

func initWs(ctx *cli.Context) {
	if !config.DefConfig.Ws.EnableHttpWs {
		return
	}
	websocket.StartServer()

	log.Infof("Ws init success")
}

func initNodeInfo(ctx *cli.Context, p2pSvr *p2pserver.P2PServer) {
	if config.DefConfig.P2PNode.HttpInfoPort == 0 {
		return
	}
	go nodeinfo.StartServer(p2pSvr.GetNetWork())

	log.Infof("Nodeinfo init success")
}

func logCurrBlockHeight(shardID common.ShardID) {
	ticker := time.NewTicker(config.DEFAULT_GEN_BLOCK_TIME * time.Second)
	for {
		select {
		case <-ticker.C:
			heights := make(map[uint64]uint32)
			for id := shardID; !id.IsRootShard(); id = id.ParentID() {
				if lgr := ledger.GetShardLedger(id); lgr != nil {
					heights[id.ToUint64()] = lgr.GetCurrentBlockHeight()
				}
			}

			if rootLgr := ledger.GetShardLedger(common.NewShardIDUnchecked(config.DEFAULT_SHARD_ID)); rootLgr != nil {
				heights[config.DEFAULT_SHARD_ID] = rootLgr.GetCurrentBlockHeight()
			}

			log.Infof("CurrentBlockHeight = %v", heights)
			isNeedNewFile := log.CheckIfNeedNewFile()
			if isNeedNewFile {
				log.ClosePrintLog()
				log.InitLog(int(config.DefConfig.Common.LogLevel), log.PATH, log.Stdout)
			}
		}
	}
}

func setMaxOpenFiles() {
	max, err := fdlimit.Maximum()
	if err != nil {
		log.Errorf("failed to get maximum open files:%v", err)
		return
	}
	_, err = fdlimit.Raise(uint64(max))
	if err != nil {
		log.Errorf("failed to set maximum open files:%v", err)
		return
	}
}

func waitToExit() {
	exit := make(chan bool, 0)
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sc {
			log.Infof("Ontology received exit signal:%v.", sig.String())
			close(exit)
			break
		}
	}()
	<-exit
}
