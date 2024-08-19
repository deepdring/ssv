package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ssvlabs/ssv/e2e/logs_catcher/matchers"
	"github.com/ssvlabs/ssv/networkconfig"
	"os"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/e2e/logs_catcher"
	"github.com/ssvlabs/ssv/e2e/logs_catcher/docker"
)

type LogsCatcherCmd struct {
	Mode string `required:"" env:"Mode" help:"Mode of the logs catcher. Can be Slashing or BlsVerification"`
}

type BlsVerificationJSON struct {
	CorruptedShares []*matchers.CorruptedShare `json:"bls_verification"`
}

func (cmd *LogsCatcherCmd) Run(logger *zap.Logger, globals Globals) error {
	// TODO: where do we stop?
	ctx := context.Background()

	cli, err := docker.New()
	if err != nil {
		return fmt.Errorf("failed to open docker client: %w", err)
	}
	defer cli.Close()

	//TODO: run fataler and matcher in parallel?

	// Execute different logic based on the value of the Mode flag
	networkCfg := networkconfig.HoleskyE2E
	dutyMatcher := matchers.NewDutyMatcher(logger, cli, ctx, networkCfg.PastAlanFork())
	switch cmd.Mode {
	case logs_catcher.SlashingMode:
		logger.Info("Running slashing mode")
		err = logs_catcher.FatalListener(ctx, logger, cli)
		if err != nil {
			return err
		}
		err = dutyMatcher.Match()
		if err != nil {
			return err
		}

	case logs_catcher.BlsVerificationMode:
		logger.Info("Running BlsVerification mode")

		corruptedShares, err := UnmarshalBlsVerificationJSON(globals.ValidatorsFile)
		if err != nil {
			return fmt.Errorf("failed to unmarshal bls verification json: %w", err)
		}

		for _, corruptedShare := range corruptedShares {
			if err = matchers.VerifyBLSSignature(ctx, logger, cli, corruptedShare); err != nil {
				return fmt.Errorf("failed to verify BLS signature for validator index %d: %w", corruptedShare.ValidatorIndex, err)
			}
		}

	default:
		return fmt.Errorf("invalid mode: %s", cmd.Mode)
	}

	return nil
}

// UnmarshalBlsVerificationJSON reads the JSON file and unmarshals it into []*CorruptedShare.
func UnmarshalBlsVerificationJSON(filePath string) ([]*matchers.CorruptedShare, error) {
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("error reading json file for BLS verification: %s, %w", filePath, err)
	}

	var blsVerificationJSON BlsVerificationJSON
	if err = json.Unmarshal(contents, &blsVerificationJSON); err != nil {
		return nil, fmt.Errorf("error parsing json file for BLS verification: %s, %w", filePath, err)
	}

	return blsVerificationJSON.CorruptedShares, nil
}
