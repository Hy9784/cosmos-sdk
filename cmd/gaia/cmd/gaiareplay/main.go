package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"

	abci "github.com/tendermint/tendermint/abci/types"
	bcm "github.com/tendermint/tendermint/blockchain"
	dbm "github.com/tendermint/tendermint/libs/db"
	"github.com/tendermint/tendermint/proxy"
	tmsm "github.com/tendermint/tendermint/state"
	tm "github.com/tendermint/tendermint/types"

	"github.com/cosmos/cosmos-sdk/cmd/gaia/app"
	"github.com/cosmos/cosmos-sdk/server"
)

func main() {
	rootDir := "data"
	ctx := server.NewDefaultContext()

	// App DB
	// appDB := dbm.NewMemDB()
	appDB, err := dbm.NewGoLevelDB("app", rootDir)
	if err != nil {
		panic(err)
	}

	// TM DB
	// tmDB := dbm.NewMemDB()
	tmDB, err := dbm.NewGoLevelDB("tendermint", rootDir)
	if err != nil {
		panic(err)
	}

	// Blockchain DB
	bcDB, err := dbm.NewGoLevelDB("blockstore", rootDir)
	if err != nil {
		panic(err)
	}

	// TraceStore
	var traceStoreWriter io.Writer
	var traceStoreDir = filepath.Join(rootDir, "trace.log")
	traceStoreWriter, err = os.OpenFile(
		traceStoreDir,
		os.O_WRONLY|os.O_APPEND|os.O_CREATE,
		0666,
	)
	if err != nil {
		panic(err)
	}

	// Application
	myapp := app.NewGaiaApp(
		ctx.Logger, appDB, traceStoreWriter,
		baseapp.SetPruning("nothing"),
	)

	// Genesis
	var genDocPath = filepath.Join(rootDir, "genesis.json")
	genDoc, err := tm.GenesisDocFromFile(genDocPath)
	if err != nil {
		panic(err)
	}
	genState, err := tmsm.MakeGenesisState(genDoc)
	if err != nil {
		panic(err)
	}
	// tmsm.SaveState(tmDB, genState)

	cc := proxy.NewLocalClientCreator(myapp)
	proxyApp := proxy.NewAppConns(cc, nil)
	err = proxyApp.Start()
	if err != nil {
		panic(err)
	}
	defer proxyApp.Stop()

	// Send InitChain msg
	validators := tm.TM2PB.Validators(genState.Validators)
	csParams := tm.TM2PB.ConsensusParams(genDoc.ConsensusParams)
	req := abci.RequestInitChain{
		Time:            genDoc.GenesisTime.Unix(),
		ChainId:         genDoc.ChainID,
		ConsensusParams: csParams,
		Validators:      validators,
		AppStateBytes:   genDoc.AppState,
	}
	_, err = proxyApp.Consensus().InitChainSync(req)
	if err != nil {
		panic(err)
	}

	// Create executor
	blockExec := tmsm.NewBlockExecutor(tmDB, ctx.Logger, proxyApp.Consensus(),
		tmsm.MockMempool{}, tmsm.MockEvidencePool{})

	// Create block store
	blockStore := bcm.NewBlockStore(bcDB)

	// Update this state.
	state := genState
	tz := []time.Duration{0, 0, 0}
	for i := 1; i < 1e10; i++ {

		t1 := time.Now()

		// Apply block
		fmt.Printf("loading and applying block %d\n", i)
		blockmeta := blockStore.LoadBlockMeta(int64(i))
		if blockmeta == nil {
			panic(fmt.Sprintf("couldn't find block meta %d", i))
		}
		block := blockStore.LoadBlock(int64(i))
		if block == nil {
			panic(fmt.Sprintf("couldn't find block %d", i))
		}

		t2 := time.Now()

		state, err = blockExec.ApplyBlock(state, blockmeta.BlockID, block)
		if err != nil {
			panic(err)
		}

		t3 := time.Now()
		tz[0] += t2.Sub(t1)
		tz[1] += t3.Sub(t2)

		fmt.Printf("new app hash: %X\n", state.AppHash)
		fmt.Println(tz)
	}

}