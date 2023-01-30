package main

import (
	"github.com/go-co-op/gocron"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"lido-near-client/internal/application"
	"lido-near-client/internal/config"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	err := os.Setenv("TZ", "UTC")
	if err != nil {
		log.Fatalf("os.Setenv (TZ): %s", err.Error())
	}
	app := &cli.App{
		Action:   mainCommand,
		Commands: nil,
	}
	err = app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func mainCommand(ctxCli *cli.Context) error {
	ctx, stop := signal.NotifyContext(ctxCli.Context, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := config.GetConfig()
	if err != nil {
		return errors.Wrap(err, "get config")
	}
	logger := getLogger(cfg.LogLevel)

	app, err := application.New(application.Params{
		Ctx: ctx,
		Log: logger,
		Cfg: cfg,
	})
	if err != nil {
		logger.Fatal("new application", zap.Error(err))
	}

	if err != nil {
		logger.Fatal("new http rest server", zap.Error(err))
	}
	startCron(app, logger)
	<-ctx.Done()
	return nil
}

func startCron(app *application.Application, logger *zap.Logger) {
	cron := gocron.NewScheduler(time.UTC)
	cron.Every(10).Minutes().Do(func() {
		err := app.StakePool.PoolUpdate()
		if err != nil {
			logger.Error("PoolUpdate", zap.Error(err))
			return
		}
	})
	cron.Every(10).Minutes().Do(func() {
		err := app.StakePool.IncreaseStake()
		if err != nil {
			logger.Error("IncreaseStake", zap.Error(err))
			return
		}
	})
	cron.StartAsync()
}

func getLogger(lvl string) *zap.Logger {
	atom := zap.NewAtomicLevel()

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "time"
	encoderCfg.LevelKey = "lvl"
	encoderCfg.EncodeTime = zapcore.RFC3339TimeEncoder

	switch lvl {
	case "debug":
		atom.SetLevel(zap.DebugLevel)
	case "info":
		atom.SetLevel(zap.InfoLevel)
	case "error":
		atom.SetLevel(zap.ErrorLevel)
	}

	return zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	), zap.AddStacktrace(zap.DPanicLevel), zap.AddCallerSkip(0))
}
