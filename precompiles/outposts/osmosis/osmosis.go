package osmosis

import (
	"bytes"
	"embed"
	"fmt"

	"github.com/cometbft/cometbft/libs/log"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	clienttypes "github.com/cosmos/ibc-go/v7/modules/core/02-client/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	cmn "github.com/evmos/evmos/v14/precompiles/common"
	erc20keeper "github.com/evmos/evmos/v14/x/erc20/keeper"
	erc20types "github.com/evmos/evmos/v14/x/erc20/types"
	transferkeeper "github.com/evmos/evmos/v14/x/ibc/transfer/keeper"
)

const (
	// OsmosisChannelIDMainnet is the channel ID for the Osmosis channel on Evmos mainnet.
	OsmosisChannelIDMainnet = "channel-0"
	// OsmosisChannelIDTestnet is the channel ID for the Osmosis channel on Evmos testnet.
	OsmosisChannelIDTestnet = "channel-0"
	OsmosisOutpostAddress   = "0x0000000000000000000000000000000000000901"
)

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type Precompile struct {
	cmn.Precompile
	portID             string
	channelID          string
	timeoutHeight      clienttypes.Height
	osmosisXCSContract string

	transferKeeper transferkeeper.Keeper
	erc20Keeper    erc20keeper.Keeper
	bankKeeper     erc20types.BankKeeper
	stakingKeeper  stakingkeeper.Keeper
}

// NewPrecompile creates a new staking Precompile instance as a
// PrecompiledContract interface.
func NewPrecompile(
	portID, channelID string,
	osmosisXCSContract string,
	transferKeeper transferkeeper.Keeper,
	authzKeeper authzkeeper.Keeper,
	bankKeeper erc20types.BankKeeper,
	erc20Keeper erc20keeper.Keeper,
) (*Precompile, error) {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		return nil, err
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		return nil, err
	}

	return &Precompile{
		Precompile: cmn.Precompile{
			ABI:                  newAbi,
			AuthzKeeper:          authzKeeper,
			KvGasConfig:          storetypes.KVGasConfig(),
			TransientKVGasConfig: storetypes.TransientGasConfig(),
			ApprovalExpiration:   cmn.DefaultExpirationDuration, // should be configurable in the future.
		},
		portID:             portID,
		channelID:          channelID,
		timeoutHeight:      clienttypes.NewHeight(100, 100),
		osmosisXCSContract: osmosisXCSContract,
		transferKeeper:     transferKeeper,
		bankKeeper:         bankKeeper,
		erc20Keeper:        erc20Keeper,
	}, nil
}

// Address defines the address of the Osmosis Outpost precompile contract.
func (Precompile) Address() common.Address {
	return common.HexToAddress(OsmosisOutpostAddress)
}

// IsStateful returns true since the precompile contract has access to the
// chain state.
func (Precompile) IsStateful() bool {
	return true
}

// RequiredGas calculates the precompiled contract's base gas rate.
func (p Precompile) RequiredGas(input []byte) uint64 {
	methodID := input[:4]

	method, err := p.MethodById(methodID)
	if err != nil {
		// This should never happen since this method is going to fail during Run
		return 0
	}

	return p.Precompile.RequiredGas(input, p.IsTransaction(method.Name))
}

// Run executes the precompiled contract Osmosis methods defined in the ABI.
func (p Precompile) Run(evm *vm.EVM, contract *vm.Contract, readOnly bool) (bz []byte, err error) {
	ctx, stateDB, method, initialGas, args, err := p.RunSetup(evm, contract, readOnly, p.IsTransaction)
	if err != nil {
		return nil, err
	}

	// This handles any out of gas errors that may occur during the execution of a precompile tx or query.
	// It avoids panics and returns the out of gas error so the EVM can continue gracefully.
	defer cmn.HandleGasError(ctx, contract, initialGas, &err)()

	switch method.Name {
	case SwapMethod:
		bz, err = p.Swap(ctx, evm.Origin, contract, stateDB, method, args)
	default:
		return nil, fmt.Errorf(cmn.ErrUnknownMethod, method.Name)
	}

	if err != nil {
		return nil, err
	}

	cost := ctx.GasMeter().GasConsumed() - initialGas

	if !contract.UseGas(cost) {
		return nil, vm.ErrOutOfGas
	}

	return bz, nil
}

// IsTransaction checks if the given method name corresponds to a transaction or query.
func (Precompile) IsTransaction(method string) bool {
	switch method {
	case SwapMethod:
		return true
	default:
		return false
	}
}

// Logger returns a precompile-specific logger.
func (p Precompile) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("outpost extension", "Osmosis")
}
