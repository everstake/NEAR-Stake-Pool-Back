package config

import (
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/pkg/errors"
)

const envPath = "./.env"

type (
	Config struct {
		LogLevel         string `split_words:"true"`
		Node             string `split_words:"true"`
		StakePool        string `split_words:"true"`
		KeyPair          string `split_words:"true"`
		KeyPairAccountID string `split_words:"true"`
	}
)

func GetConfig() (cfg Config, err error) {
	err = godotenv.Load(envPath)
	if err != nil {
		return cfg, errors.Wrap(err, "loading env file")
	}
	err = envconfig.Process("", &cfg)
	if err != nil {
		return cfg, errors.Wrap(err, "env config precess")
	}
	return cfg, nil
}
