package stakepool

import (
	"encoding/base64"
	"encoding/json"
	"github.com/eteu-technologies/near-api-go/pkg/client"
	"github.com/eteu-technologies/near-api-go/pkg/client/block"
	"github.com/eteu-technologies/near-api-go/pkg/types"
	"github.com/eteu-technologies/near-api-go/pkg/types/action"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"sort"
	"time"
)

var minRebalanceStake = decimal.New(1, 24)

func (s *Service) PoolUpdate() error {
	err := s.takeUnstakedBalance()
	if err != nil {
		return errors.Wrap(err, "takeUnstakedBalance")
	}

	var epochs EpochHeightRegistry
	err = s.callContractWithUnmarshal("get_current_epoch_height", "", &epochs)
	if err != nil {
		return errors.Wrap(err, "callContractWithUnmarshal(get_current_epoch_height)")
	}
	if epochs.PoolEpochHeight == epochs.NetworkEpochHeight {
		s.log.Debug("PoolUpdate: not yet")
		return nil
	}

	// check balance
	accRes, err := s.cli.AccountView(s.ctx, s.cfg.KeyPairAccountID, block.FinalityFinal())
	if err != nil {
		return errors.Wrap(err, "AccountView")
	}
	var accountView AccountView
	err = json.Unmarshal(accRes.Result, &accountView)
	if err != nil {
		return errors.Wrap(err, "json.Unmarshal(AccountView)")
	}
	balance := accountView.Amount.Div(decimal.New(1, 24))
	if balance.LessThan(decimal.NewFromFloat(0.01)) {
		// TODO notify
	}

	var validators []Validator
	err = s.callContractWithUnmarshal("get_validator_registry", "", &validators)
	if err != nil {
		return errors.Wrap(err, "callContractWithUnmarshal(get_validator_registry)")
	}
	for _, v := range validators {
		if v.LastUpdateEpochHeight == epochs.NetworkEpochHeight {
			s.log.Warn("PoolUpdate: validator already updated", zap.String("validator", v.AccountID))
			continue
		}
		argsMarshaled, _ := json.Marshal(map[string]string{"validator_account_id": v.AccountID})
		res, err := s.cli.TransactionSendAwait(s.ctx, s.cfg.KeyPairAccountID, s.cfg.StakePool, []action.Action{
			action.NewFunctionCall("update_validator", argsMarshaled, types.DefaultFunctionCallGas*10, types.BalanceFromFloat(0)),
		}, client.WithLatestBlock(),
			client.WithKeyPair(s.keyPair),
		)
		if err != nil {
			return errors.Wrapf(err, "TransactionSendAwait[validator:%s]", v.AccountID)
		}
		if res.Status.Failure != nil {
			return errors.New(string(res.Status.Failure))
		}
		data, _ := base64.StdEncoding.DecodeString(res.Status.SuccessValue)
		var resp CallbackResult
		err = json.Unmarshal(data, &resp)
		if err != nil {
			return errors.Wrap(err, "json.Unmarshal(resp)")
		}
		if !resp.IsSuccess {
			return errors.Errorf("fail result from validator %s", v.AccountID)
		}
		if resp.NetworkEpochHeight != epochs.NetworkEpochHeight {
			return errors.Errorf("mismatch epoch after update %d != %d", resp.NetworkEpochHeight, epochs.NetworkEpochHeight)
		}
	}

	err = s.requestedDecreaseValidatorStake()
	if err != nil {
		return errors.Wrap(err, "requestedDecreaseValidatorStake")
	}

	// update stake pool
	res, err := s.cli.TransactionSendAwait(s.ctx, s.cfg.KeyPairAccountID, s.cfg.StakePool, []action.Action{
		action.NewFunctionCall("update", nil, types.DefaultFunctionCallGas*10, types.BalanceFromFloat(0)),
	}, client.WithLatestBlock(),
		client.WithKeyPair(s.keyPair),
	)
	if err != nil {
		return errors.Wrap(err, "TransactionSendAwait(update)")
	}
	if res.Status.Failure != nil {
		return errors.Errorf("pool update %s", string(res.Status.Failure))
	}
	s.log.Info("Pool updated", zap.Int("validators", len(validators)), zap.String("tx", res.Transaction.Hash.String()))
	return nil
}

func (s *Service) getGenesisCfg() (cfg GenesisConfig, err error) {
	resp, err := s.cli.GenesisConfig(s.ctx)
	if err != nil {
		return cfg, errors.Wrap(err, "GenesisConfig")
	}
	err = json.Unmarshal(resp.Result, &cfg)
	if err != nil {
		return cfg, errors.Wrap(err, "json.Unmarshal")
	}
	return cfg, nil
}

func (s *Service) IncreaseStake() error {
	t := time.Now()

	genesis, err := s.getGenesisCfg()
	if err != nil {
		return errors.Wrap(err, "getGenesisConfig")
	}
	latestBlock, err := s.cli.BlockDetails(s.ctx, block.FinalityFinal())
	if err != nil {
		return errors.Wrap(err, "BlockDetails")
	}
	remain := latestBlock.Header.Height % genesis.EpochLength
	if remain < genesis.EpochLength-6480 { // 6480 it`s 15% of EpochLength
		s.log.Debug("IncreaseStake: not yet")
		return nil
	}

	var isDistributed bool
	err = s.callContractWithUnmarshal("is_stake_distributed", "", &isDistributed)
	if err != nil {
		return errors.Wrap(err, "callContractWithUnmarshal(is_stake_distributed)")
	}
	if isDistributed {
		s.log.Debug("IncreaseStake: already distributed")
		return nil
	}

	var epochs EpochHeightRegistry
	err = s.callContractWithUnmarshal("get_current_epoch_height", "", &epochs)
	if err != nil {
		return errors.Wrap(err, "callContractWithUnmarshal(get_current_epoch_height)")
	}

	if epochs.NetworkEpochHeight != epochs.PoolEpochHeight {
		return errors.Wrap(err, "epochs are different")
	}

	var validators []Validator
	err = s.callContractWithUnmarshal("get_validator_registry", "", &validators)
	if err != nil {
		return errors.Wrap(err, "callContractWithUnmarshal(get_validator_registry)")
	}

	var filteredValidators []Validator
	for _, validator := range validators {
		if validator.IsOnlyForInvestment {
			continue
		}
		if validator.LastClassicStakeIncreasingEpochHeight == nil || *validator.LastClassicStakeIncreasingEpochHeight < epochs.PoolEpochHeight {
			filteredValidators = append(filteredValidators, validator)
		}
	}

	var fund Fund
	err = s.callContractWithUnmarshal("get_fund", "", &fund)
	if err != nil {
		return errors.Wrap(err, "callContractWithUnmarshal(get_fund)")
	}

	if fund.ClassicUnstakedBalance.IsZero() {
		s.log.Info("IncreaseStake: ClassicUnstakedBalance is zero")
		return nil
	}

	if len(filteredValidators) == 0 {
		s.log.Info("IncreaseStake: not found available validators")
		return nil
	}

	type validatorShare struct {
		validator Validator
		stake     decimal.Decimal
	}
	var shares []validatorShare
	part := fund.ClassicUnstakedBalance.Div(decimal.New(int64(len(filteredValidators)), 0)).Truncate(0)
	if part.LessThan(minRebalanceStake) {
		numberOfValidators := fund.ClassicUnstakedBalance.Div(minRebalanceStake).IntPart()
		for i := 0; i < int(numberOfValidators); i++ {
			shares = append(shares, validatorShare{
				validator: filteredValidators[i],
				stake:     part,
			})
		}
		modAmount := fund.ClassicUnstakedBalance.Mod(minRebalanceStake)
		shares[len(shares)-1].stake = shares[len(shares)-1].stake.Add(modAmount)
	} else {
		for i := 0; i < len(filteredValidators); i++ {
			shares = append(shares, validatorShare{
				validator: filteredValidators[i],
				stake:     part,
			})
		}
		modAmount := fund.ClassicUnstakedBalance.Mod(decimal.New(int64(len(filteredValidators)), 0))
		shares[len(shares)-1].stake = shares[len(shares)-1].stake.Add(modAmount)
	}

	// todo make equal sharing via all staking balance
	for _, share := range shares {
		argsMarshaled, _ := json.Marshal(map[string]interface{}{
			"validator_account_id": share.validator.AccountID,
			"near_amount":          share.stake.Truncate(0).BigInt().String(),
		})
		res, err := s.cli.TransactionSendAwait(s.ctx, s.cfg.KeyPairAccountID, s.cfg.StakePool, []action.Action{
			action.NewFunctionCall("increase_validator_stake", argsMarshaled, types.DefaultFunctionCallGas*10, types.BalanceFromFloat(0)),
		}, client.WithLatestBlock(),
			client.WithKeyPair(s.keyPair),
		)
		if err != nil {
			return errors.Wrap(err, "TransactionSendAwait(increase_validator_stake)")
		}
		if res.Status.Failure != nil {
			return errors.Errorf("increase validator stake %s", string(res.Status.Failure))
		}
		data, _ := base64.StdEncoding.DecodeString(res.Status.SuccessValue)
		var resp bool
		err = json.Unmarshal(data, &resp)
		if err != nil {
			return errors.Wrap(err, "json.Unmarshal(resp)")
		}
		s.log.Info(
			"IncreaseStake: call increase_validator_stake",
			zap.String("validator", share.validator.AccountID),
			zap.String("amount", share.stake.String()),
			zap.Bool("response", resp),
			zap.String("tx_hash", res.Transaction.Hash.String()),
		)
		if !resp {
			return errors.New("false result")
		}
	}

	res, err := s.cli.TransactionSendAwait(s.ctx, s.cfg.KeyPairAccountID, s.cfg.StakePool, []action.Action{
		action.NewFunctionCall("confirm_stake_distribution", nil, types.DefaultFunctionCallGas*10, types.BalanceFromFloat(0)),
	}, client.WithLatestBlock(),
		client.WithKeyPair(s.keyPair),
	)
	if err != nil {
		return errors.Wrap(err, "TransactionSendAwait(confirm_stake_distribution)")
	}
	if res.Status.Failure != nil {
		return errors.Errorf("confirm stake distribution %s", string(res.Status.Failure))
	}

	s.log.Info("IncreaseStake: confirmed", zap.Duration("duration", time.Now().Sub(t)))
	return nil
}

type (
	RequestedToWithdrawalFund struct {
		ClassicNearAmount            decimal.Decimal `json:"classic_near_amount"`
		InvestmentNearAmount         decimal.Decimal `json:"investment_near_amount"`
		InvestmentWithdrawalRegistry [][]interface{} `json:"investment_withdrawal_registry"`
	}
)

func (s *Service) requestedDecreaseValidatorStake() error {
	var epochs EpochHeightRegistry
	err := s.callContractWithUnmarshal("get_current_epoch_height", "", &epochs)
	if err != nil {
		return errors.Wrap(err, "callContractWithUnmarshal(get_current_epoch_height)")
	}
	if epochs.NetworkEpochHeight%4 != 0 || epochs.PoolEpochHeight >= epochs.NetworkEpochHeight {
		s.log.Debug("requestedDecreaseValidatorStake: not yet")
		return nil
	}

	var requestedToWithdrawalFund RequestedToWithdrawalFund
	err = s.callContractWithUnmarshal("get_requested_to_withdrawal_fund", "", &requestedToWithdrawalFund)
	if err != nil {
		return errors.Wrap(err, "callContractWithUnmarshal(get_requested_to_withdrawal_fund)")
	}

	var validators []Validator
	err = s.callContractWithUnmarshal("get_validator_registry", "", &validators)
	if err != nil {
		return errors.Wrap(err, "callContractWithUnmarshal(get_validator_registry)")
	}
	var filteredValidators []Validator
	for _, validator := range validators {
		if !validator.IsOnlyForInvestment && validator.ClassicStakedBalance.GreaterThan(decimal.Zero) {
			filteredValidators = append(filteredValidators, validator)
		}
	}

	nearAmount := requestedToWithdrawalFund.ClassicNearAmount
	for _, validator := range filteredValidators {
		if nearAmount.IsZero() {
			break
		}
		if nearAmount.GreaterThanOrEqual(validator.ClassicStakedBalance) {
			args, _ := json.Marshal(map[string]interface{}{
				"validator_account_id":  validator.AccountID,
				"near_amount":           validator.ClassicStakedBalance.BigInt().String(),
				"stake_decreasing_type": ClassicStakeDecreasingType,
			})
			res, err := s.cli.TransactionSendAwait(s.ctx, s.cfg.KeyPairAccountID, s.cfg.StakePool, []action.Action{
				action.NewFunctionCall("requested_decrease_validator_stake", args, types.DefaultFunctionCallGas*10, types.BalanceFromFloat(0)),
			}, client.WithLatestBlock(),
				client.WithKeyPair(s.keyPair),
			)
			if err != nil {
				return errors.Wrap(err, "TransactionSendAwait(requested_decrease_validator_stake)")
			}
			if res.Status.Failure != nil {
				return errors.Errorf("requested decrease validator stake: %s", string(res.Status.Failure))
			}
			data, _ := base64.StdEncoding.DecodeString(res.Status.SuccessValue)
			var resp CallbackResult
			err = json.Unmarshal(data, &resp)
			if err != nil {
				return errors.Wrap(err, "json.Unmarshal(resp)")
			}
			if !resp.IsSuccess {
				return errors.Errorf("fail result from validator %s", validator.AccountID)
			}
			if resp.NetworkEpochHeight != epochs.NetworkEpochHeight {
				return errors.Errorf("mismatch epoch after update %d != %d", resp.NetworkEpochHeight, epochs.NetworkEpochHeight)
			}
			nearAmount = nearAmount.Sub(validator.ClassicStakedBalance)
		} else {
			args, _ := json.Marshal(map[string]interface{}{
				"validator_account_id":  validator.AccountID,
				"near_amount":           nearAmount.BigInt().String(),
				"stake_decreasing_type": ClassicStakeDecreasingType,
			})
			res, err := s.cli.TransactionSendAwait(s.ctx, s.cfg.KeyPairAccountID, s.cfg.StakePool, []action.Action{
				action.NewFunctionCall("requested_decrease_validator_stake", args, types.DefaultFunctionCallGas*10, types.BalanceFromFloat(0)),
			}, client.WithLatestBlock(),
				client.WithKeyPair(s.keyPair),
			)
			if err != nil {
				return errors.Wrap(err, "TransactionSendAwait(requested_decrease_validator_stake)")
			}
			if res.Status.Failure != nil {
				return errors.Errorf("requested decrease validator stake: %s", string(res.Status.Failure))
			}
			data, _ := base64.StdEncoding.DecodeString(res.Status.SuccessValue)
			var resp CallbackResult
			err = json.Unmarshal(data, &resp)
			if err != nil {
				return errors.Wrap(err, "json.Unmarshal(resp)")
			}
			if !resp.IsSuccess {
				return errors.Errorf("fail result from validator %s", validator.AccountID)
			}
			if resp.NetworkEpochHeight != epochs.NetworkEpochHeight {
				return errors.Errorf("mismatch epoch after update %d != %d", resp.NetworkEpochHeight, epochs.NetworkEpochHeight)
			}
			nearAmount = decimal.Zero
		}
	}

	for _, v := range requestedToWithdrawalFund.InvestmentWithdrawalRegistry {
		if len(v) != 2 {
			return errors.Errorf("unknown format of InvestmentWithdrawalRegistry")
		}
		investmentWithdrawalRegistryAccountID, ok := v[0].(string)
		if !ok {
			return errors.Errorf("unknown format of InvestmentWithdrawalRegistry(AccountId)")
		}
		investmentWithdrawalRegistryAmountStr, ok := v[1].(string)
		if !ok {
			return errors.Errorf("unknown format of InvestmentWithdrawalRegistry(Amount)")
		}
		investmentWithdrawalRegistryAmount, err := decimal.NewFromString(investmentWithdrawalRegistryAmountStr)
		if err != nil {
			return errors.Wrap(err, "decimal.NewFromString(investmentWithdrawalRegistryAmountStr)")
		}
		args, _ := json.Marshal(map[string]interface{}{
			"validator_account_id":  investmentWithdrawalRegistryAccountID,
			"near_amount":           investmentWithdrawalRegistryAmount.BigInt().String(),
			"stake_decreasing_type": InvestmentStakeDecreasingType,
		})
		res, err := s.cli.TransactionSendAwait(s.ctx, s.cfg.KeyPairAccountID, s.cfg.StakePool, []action.Action{
			action.NewFunctionCall("requested_decrease_validator_stake", args, types.DefaultFunctionCallGas*10, types.BalanceFromFloat(0)),
		}, client.WithLatestBlock(),
			client.WithKeyPair(s.keyPair),
		)
		if err != nil {
			return errors.Wrap(err, "TransactionSendAwait(requested_decrease_validator_stake)")
		}
		if res.Status.Failure != nil {
			return errors.Errorf("requested decrease validator stake: %s", string(res.Status.Failure))
		}
		data, _ := base64.StdEncoding.DecodeString(res.Status.SuccessValue)
		var resp CallbackResult
		err = json.Unmarshal(data, &resp)
		if err != nil {
			return errors.Wrap(err, "json.Unmarshal(resp)")
		}
		if !resp.IsSuccess {
			return errors.Errorf("fail result from validator %s", investmentWithdrawalRegistryAccountID)
		}
		if resp.NetworkEpochHeight != epochs.NetworkEpochHeight {
			return errors.Errorf("mismatch epoch after update %d != %d", resp.NetworkEpochHeight, epochs.NetworkEpochHeight)
		}
	}
	return nil
}

func (s *Service) takeUnstakedBalance() error {
	var epochs EpochHeightRegistry
	err := s.callContractWithUnmarshal("get_current_epoch_height", "", &epochs)
	if err != nil {
		return errors.Wrap(err, "callContractWithUnmarshal(get_current_epoch_height)")
	}
	if epochs.NetworkEpochHeight%4 != 0 || epochs.PoolEpochHeight >= epochs.NetworkEpochHeight {
		s.log.Debug("takeUnstakedBalance: not yet")
		return nil
	}
	var validators []Validator
	err = s.callContractWithUnmarshal("get_validator_registry", "", &validators)
	if err != nil {
		return errors.Wrap(err, "callContractWithUnmarshal(get_validator_registry)")
	}
	for _, validator := range validators {
		if validator.LastUpdateEpochHeight == epochs.NetworkEpochHeight {
			continue
		}
		if validator.UnstakedBalance.GreaterThan(decimal.Zero) {
			argsMarshaled, _ := json.Marshal(map[string]interface{}{
				"validator_account_id": validator.AccountID,
			})
			res, err := s.cli.TransactionSendAwait(s.ctx, s.cfg.KeyPairAccountID, s.cfg.StakePool, []action.Action{
				action.NewFunctionCall("take_unstaked_balance", argsMarshaled, types.DefaultFunctionCallGas*10, types.BalanceFromFloat(0)),
			}, client.WithLatestBlock(),
				client.WithKeyPair(s.keyPair),
			)
			if err != nil {
				return errors.Wrap(err, "TransactionSendAwait(take_unstaked_balance)")
			}
			if res.Status.Failure != nil {
				return errors.Errorf("decrease validator stake: %s", string(res.Status.Failure))
			}
			data, _ := base64.StdEncoding.DecodeString(res.Status.SuccessValue)
			var resp CallbackResult
			err = json.Unmarshal(data, &resp)
			if err != nil {
				return errors.Wrap(err, "json.Unmarshal(resp)")
			}
			if !resp.IsSuccess {
				return errors.Errorf("fail result from validator %s", validator.AccountID)
			}
			if resp.NetworkEpochHeight != epochs.NetworkEpochHeight {
				return errors.Errorf("mismatch epoch after update %d != %d", resp.NetworkEpochHeight, epochs.NetworkEpochHeight)
			}
		}
	}
	return nil
}

func stakeDistribution(stakeRemains decimal.Decimal, shares map[string]decimal.Decimal, validators []Validator) {
	if len(validators) == 0 {
		return
	}

	copiedValidators := make([]Validator, len(validators))
	copy(copiedValidators, validators)

	// fill had been already distributed shares and find max stake
	max := copiedValidators[0].ClassicStakedBalance
	for i, v := range copiedValidators {
		share, ok := shares[v.AccountID]
		if ok {
			copiedValidators[i].ClassicStakedBalance = v.ClassicStakedBalance.Add(share)
		}
		if copiedValidators[i].ClassicStakedBalance.GreaterThan(max) {
			max = copiedValidators[i].ClassicStakedBalance.Copy()
		}
	}

	// sorting (asc)
	sort.Slice(copiedValidators, func(i, j int) bool {
		return copiedValidators[i].ClassicStakedBalance.GreaterThan(copiedValidators[j].ClassicStakedBalance)
	})

	if copiedValidators[0].ClassicStakedBalance.Equals(max) { // so all validators stake are equal
		part := stakeRemains.Div(decimal.New(int64(len(validators)), 0)).Truncate(0)
		if part.GreaterThan(minRebalanceStake) { // div equal part for everyone
			for validator, stake := range shares {
				shares[validator] = stake.Add(part)
			}
		} else { // delegate all stake remains for one validator
			shares[copiedValidators[0].AccountID] = stakeRemains
		}
		return
	}

	for _, v := range copiedValidators {
		lack := max.Sub(v.ClassicStakedBalance)
		if stakeRemains.GreaterThanOrEqual(lack) {
			shares[v.AccountID] = shares[v.AccountID].Add(lack)
			stakeRemains = stakeRemains.Sub(lack)
		} else {
			if stakeRemains.GreaterThan(decimal.Zero) {
				shares[v.AccountID] = shares[v.AccountID].Add(stakeRemains)
				stakeRemains = decimal.Zero
			}
			return
		}
	}

	if stakeRemains.Equal(decimal.Zero) {
		return
	}
	stakeDistribution(stakeRemains, shares, copiedValidators)
}
