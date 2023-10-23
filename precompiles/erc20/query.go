// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package erc20

import (
	"errors"
	"fmt"
	"math"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/evmos/evmos/v15/precompiles/authorization"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

const (
	// NameMethod defines the ABI method name for the ERC20 Name
	// query.
	NameMethod = "name"
	// SymbolMethod defines the ABI method name for the ERC20 Symbol
	// query.
	SymbolMethod = "symbol"
	// DecimalsMethod defines the ABI method name for the ERC20 Decimals
	// query.
	DecimalsMethod = "decimals"
	// TotalSupplyMethod defines the ABI method name for the ERC20 TotalSupply
	// query.
	TotalSupplyMethod = "totalSupply"
	// BalanceOfMethod defines the ABI method name for the ERC20 BalanceOf
	// query.
	BalanceOfMethod = "balanceOf"
)

// Name returns the name of the token.
func (p Precompile) Name(
	ctx sdk.Context,
	_ *vm.Contract,
	_ vm.StateDB,
	method *abi.Method,
	_ []interface{},
) ([]byte, error) {
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, p.tokenPair.Denom)
	if !found {
		// TODO: assume the symbol is the title case version of the IBC DenomTrace BaseDenom
		denom := p.tokenPair.Denom // FIXME: use denomTrace.BaseDenom
		return method.Outputs.Pack(strings.ToUpper(string(denom[1])) + denom[2:])
	}

	return method.Outputs.Pack(metadata.Name)
}

// Symbol returns the symbol of the token.
func (p Precompile) Symbol(
	ctx sdk.Context,
	_ *vm.Contract,
	_ vm.StateDB,
	method *abi.Method,
	_ []interface{},
) ([]byte, error) {
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, p.tokenPair.Denom)
	if !found {
		// TODO: assume the symbol is the uppercase version of the IBC DenomTrace BaseDenom
		denom := p.tokenPair.Denom // FIXME: use denomTrace.BaseDenom
		return method.Outputs.Pack(strings.ToUpper(denom[1:]))
	}

	return method.Outputs.Pack(metadata.Symbol)
}

// Decimals returns the decimals places of the token.
func (p Precompile) Decimals(
	ctx sdk.Context,
	_ *vm.Contract,
	_ vm.StateDB,
	method *abi.Method,
	_ []interface{},
) ([]byte, error) {
	metadata, found := p.bankKeeper.GetDenomMetaData(ctx, p.tokenPair.Denom)
	if !found {
		// we assume the decimal from the first character of the denomination
		switch string(p.tokenPair.Denom[0]) { // FIXME: use denomTrace.BaseDenom[0]
		case "u":
			return method.Outputs.Pack(uint8(6))
		case "a":
			return method.Outputs.Pack(uint8(18))
		}
		return nil, banktypes.ErrDenomMetadataNotFound
	}

	var decimals uint32
	for i := len(metadata.DenomUnits); i >= 0; i-- {
		if metadata.DenomUnits[i].Denom == metadata.Display {
			decimals = metadata.DenomUnits[i].Exponent
			break
		}
	}

	if decimals > math.MaxUint8 {
		return nil, errors.New("uint8 overflow: invalid decimals")
	}

	return method.Outputs.Pack(uint8(decimals))
}

// TotalSupply returns the amount of tokens in existence.
func (p Precompile) TotalSupply(
	ctx sdk.Context,
	_ *vm.Contract,
	_ vm.StateDB,
	method *abi.Method,
	_ []interface{},
) ([]byte, error) {
	supply := p.bankKeeper.GetSupply(ctx, p.tokenPair.Denom)

	return method.Outputs.Pack(supply.Amount.BigInt())
}

// BalanceOf returns the amount of tokens owned by account.
func (p Precompile) BalanceOf(
	ctx sdk.Context,
	_ *vm.Contract,
	_ vm.StateDB,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("invalid number of arguments; expected 1; got: %d", len(args))
	}

	account, ok := args[0].(common.Address)
	if !ok {
		return nil, fmt.Errorf("invalid account address: %v", args[0])
	}

	balance := p.bankKeeper.GetBalance(ctx, sdk.AccAddress(account.Bytes()), p.tokenPair.Denom)

	return method.Outputs.Pack(balance.Amount.BigInt())
}

// Allowance returns the remaining allowance of a spender to the contract
func (p Precompile) Allowance(
	ctx sdk.Context,
	_ *vm.Contract,
	_ vm.StateDB,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	owner, spender, err := ParseAllowanceArgs(args)
	if err != nil {
		return nil, err
	}

	granter := owner
	grantee := spender

	authorization, _, err := authorization.CheckAuthzExists(ctx, p.authzKeeper, grantee, granter, SendMsgURL)
	// TODO: return error if doesn't exist?
	if err != nil {
		return method.Outputs.Pack(common.Big0)
	}

	sendAuth, ok := authorization.(*banktypes.SendAuthorization)
	if !ok {
		// TODO: return error if invalid authorization?
		return method.Outputs.Pack(common.Big0)
	}

	return method.Outputs.Pack(sendAuth.SpendLimit[0].Amount.BigInt())
}
