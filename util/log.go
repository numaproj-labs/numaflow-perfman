package util

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// CommitSHA variable set at build time
var CommitSHA string

func CreateLogger() *zap.Logger {
	debugMode, ok := os.LookupEnv("DEBUG")
	isDebug := ok && debugMode == "true"
	var encoderCfg zapcore.EncoderConfig

	if isDebug {
		encoderCfg = zap.NewDevelopmentEncoderConfig()
	} else {
		encoderCfg = zap.NewProductionEncoderConfig()
	}

	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.RFC3339NanoTimeEncoder

	config := zap.Config{
		Level:             zap.NewAtomicLevelAt(zap.InfoLevel),
		Development:       isDebug,
		DisableCaller:     false,
		DisableStacktrace: true,
		Sampling:          nil,
		Encoding:          "json",
		EncoderConfig:     encoderCfg,
		OutputPaths: []string{
			"stdout",
		},
		ErrorOutputPaths: []string{
			"stderr",
		},
		InitialFields: map[string]interface{}{
			"commit-sha": CommitSHA,
		},
	}

	return zap.Must(config.Build())
}
