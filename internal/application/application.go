package application

import (
	"context"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"lido-near-client/internal/application/stakepool"
	"lido-near-client/internal/config"
)

type (
	Application struct {
		StakePool StakePoolService
	}
	Params struct {
		Ctx context.Context
		Log *zap.Logger
		Cfg config.Config
	}
	StakePoolService interface {
		PoolUpdate() error
		IncreaseStake() error
	}
)

func New(params Params) (app *Application, err error) {
	p, err := stakepool.New(stakepool.ServiceParam{
		Ctx: params.Ctx,
		Cfg: params.Cfg,
		Log: params.Log,
	})
	if err != nil {
		return app, errors.Wrap(err, "new pool")
	}
	return &Application{
		StakePool: p,
	}, nil
}
