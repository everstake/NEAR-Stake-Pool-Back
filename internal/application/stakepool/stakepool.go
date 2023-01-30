package stakepool

import (
	"context"
	"encoding/json"
	"github.com/eteu-technologies/near-api-go/pkg/client"
	"github.com/eteu-technologies/near-api-go/pkg/client/block"
	"github.com/eteu-technologies/near-api-go/pkg/types/key"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"lido-near-client/internal/config"
)

const coinGeckoID = "near"

type (
	Service struct {
		ctx context.Context
		log *zap.Logger
		cfg config.Config
		cli *client.Client

		keyPair key.KeyPair
	}
	ServiceParam struct {
		Ctx context.Context
		Log *zap.Logger
		Cfg config.Config
	}
)

func New(param ServiceParam) (*Service, error) {
	node, err := client.NewClient(param.Cfg.Node)
	if err != nil {
		return nil, errors.Wrap(err, "create client")
	}
	keyPair, err := key.NewBase58KeyPair(param.Cfg.KeyPair)
	if err != nil {
		return nil, errors.Wrap(err, "NewBase58KeyPair")
	}
	return &Service{
		ctx:     param.Ctx,
		log:     param.Log,
		cfg:     param.Cfg,
		cli:     &node,
		keyPair: keyPair,
	}, nil
}

type callContractResponse struct {
	BlockHash   string `json:"block_hash"`
	BlockHeight uint64 `json:"block_height"`
	Result      []byte `json:"result"`
	Error       string `json:"error,omitempty"`
}

func (s *Service) callContract(method string, args string) (result json.RawMessage, err error) {
	resp, err := s.cli.ContractViewCallFunction(
		context.Background(),
		s.cfg.StakePool,
		method,
		args,
		block.FinalityFinal(),
	)
	if err != nil {
		return result, errors.Wrap(err, "ContractViewCallFunction")
	}
	var r callContractResponse
	err = json.Unmarshal(resp.Result, &r)
	if err != nil {
		return result, errors.Wrap(err, "json unmarshal")
	}
	if r.Error != "" {
		return result, errors.New(r.Error)
	}
	return r.Result, nil
}

func (s *Service) callContractWithUnmarshal(method string, args string, dst interface{}) error {
	result, err := s.callContract(method, args)
	if err != nil {
		return errors.Wrap(err, "callContract")
	}
	err = json.Unmarshal(result, dst)
	if err != nil {
		return errors.Wrap(err, "json.Unmarshal")
	}
	return nil
}
