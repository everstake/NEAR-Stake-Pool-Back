package stakepool

import (
	"github.com/eteu-technologies/near-api-go/pkg/types"
	"github.com/shopspring/decimal"
)

const (
	ClassicStakeDecreasingType    stakeDecreasingType = "Classic"
	InvestmentStakeDecreasingType stakeDecreasingType = "Investment"
)

type (
	stakeDecreasingType string
	Validator           struct {
		AccountID                             types.AccountID `json:"account_id"`
		ClassicStakedBalance                  decimal.Decimal `json:"classic_staked_balance"`
		InvestmentStakedBalance               decimal.Decimal `json:"investment_staked_balance"`
		UnstakedBalance                       decimal.Decimal `json:"unstaked_balance"`
		IsOnlyForInvestment                   bool            `json:"is_only_for_investment"`
		LastUpdateEpochHeight                 uint64          `json:"last_update_epoch_height"`
		LastClassicStakeIncreasingEpochHeight *uint64         `json:"last_classic_stake_increasing_epoch_height"`
	}
	AccountView struct {
		Amount    decimal.Decimal `json:"amount"`
		BlockHash string          `json:"block_hash"`
	}
	GenesisConfig struct {
		EpochLength   uint64 `json:"epoch_length"`
		GenesisHeight uint64 `json:"genesis_height"`
	}
	AggInfo struct {
		UnstakedBalance                      decimal.Decimal `json:"unstaked_balance"`
		StakedBalance                        decimal.Decimal `json:"staked_balance"`
		TokenTotalSupply                     decimal.Decimal `json:"token_total_supply"`
		TokenAccountsQuantity                uint64          `json:"token_accounts_quantity"`
		TotalRewardsFromValidatorsNearAmount decimal.Decimal `json:"total_rewards_from_validators_near_amount"`
		RewardFee                            *Dividing       `json:"reward_fee"`
	}
	Dividing struct {
		Numerator   decimal.Decimal `json:"numerator"`
		Denominator decimal.Decimal `json:"denominator"`
	}

	Fund struct {
		ClassicUnstakedBalance  decimal.Decimal `json:"classic_unstaked_balance"`
		ClassicStakedBalance    decimal.Decimal `json:"classic_staked_balance"`
		InvestmentStakedBalance decimal.Decimal `json:"investment_staked_balance"`
		CommonStakedBalance     decimal.Decimal `json:"common_staked_balance"`
		CommonBalance           decimal.Decimal `json:"common_balance"`
	}

	EpochHeightRegistry struct {
		PoolEpochHeight    uint64 `json:"pool_epoch_height"`
		NetworkEpochHeight uint64 `json:"network_epoch_height"`
	}
	CallbackResult struct {
		IsSuccess          bool   `json:"is_success"`
		NetworkEpochHeight uint64 `json:"network_epoch_height"`
	}
)

func (v *Dividing) GetValue() decimal.Decimal {
	if v.Denominator.IsZero() {
		return v.Denominator
	}
	return v.Numerator.Div(v.Denominator)
}
