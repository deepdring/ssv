package runner

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/attestantio/go-eth2-client/spec"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner/metrics"
)

type ProposerRunner struct {
	BaseRunner *BaseRunner
	// ProducesBlindedBlocks is true when the runner will only produce blinded blocks
	ProducesBlindedBlocks bool

	beacon   specssv.BeaconNode
	network  specssv.Network
	signer   spectypes.KeyManager
	valCheck specqbft.ProposedValueCheckF

	metrics  metrics.ConsensusMetrics
	graffiti []byte
}

func NewProposerRunner(
	beaconNetwork spectypes.BeaconNetwork,
	share *spectypes.Share,
	qbftController *controller.Controller,
	beacon specssv.BeaconNode,
	network specssv.Network,
	signer spectypes.KeyManager,
	valCheck specqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
	graffiti []byte,
) Runner {
	return &ProposerRunner{
		BaseRunner: &BaseRunner{
			BeaconRoleType:     spectypes.BNRoleProposer,
			BeaconNetwork:      beaconNetwork,
			Share:              share,
			QBFTController:     qbftController,
			highestDecidedSlot: highestDecidedSlot,
		},

		beacon:   beacon,
		network:  network,
		signer:   signer,
		valCheck: valCheck,
		metrics:  metrics.NewConsensusMetrics(spectypes.BNRoleProposer),
		graffiti: graffiti,
	}
}

func (r *ProposerRunner) StartNewDuty(logger *zap.Logger, duty *spectypes.Duty) error {
	return r.BaseRunner.baseStartNewDuty(logger, r, duty)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *ProposerRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *ProposerRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg *spectypes.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing randao message")
	}

	duty := r.GetState().StartingDuty
	logger = logger.With(fields.Slot(duty.Slot))
	logger.Debug("🧩 got partial RANDAO signatures",
		zap.Uint64("signer", signedMsg.Signer))

	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	r.metrics.EndPreConsensus()

	// only 1 root, verified in basePreConsensusMsgProcessing
	root := roots[0]
	// randao is relevant only for block proposals, no need to check type
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}

	logger.Debug("🧩 reconstructed partial RANDAO signatures",
		zap.Uint64s("signers", getPreConsensusSigners(r.GetState(), root)))

	var ver spec.DataVersion
	var obj ssz.Marshaler
	var start = time.Now()
	if r.ProducesBlindedBlocks {
		// get block data
		obj, ver, err = r.GetBeaconNode().GetBlindedBeaconBlock(duty.Slot, r.graffiti, fullSig)
		if err != nil {
			return errors.Wrap(err, "failed to get blinded beacon block")
		}
	} else {
		// get block data
		obj, ver, err = r.GetBeaconNode().GetBeaconBlock(duty.Slot, r.graffiti, fullSig)
		if err != nil {
			return errors.Wrap(err, "failed to get beacon block")
		}
	}
	took := time.Since(start)
	// Log essentials about the retrieved block.
	blockSummary, summarizeErr := summarizeBlock(obj)
	logger.Info("🧊 got beacon block proposal",
		zap.String("block_hash", blockSummary.Hash.String()),
		zap.Bool("blinded", blockSummary.Blinded),
		zap.Duration("took", took),
		zap.NamedError("summarize_err", summarizeErr))

	byts, err := obj.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal beacon block")
	}

	input := &spectypes.ConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	r.metrics.StartConsensus()
	if err := r.BaseRunner.decide(logger, r, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}

func (r *ProposerRunner) ProcessConsensus(logger *zap.Logger, signedMsg *specqbft.SignedMessage) error {
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}
	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.metrics.EndConsensus()
	r.metrics.StartPostConsensus()

	// specific duty sig
	var blkToSign ssz.HashRoot
	if r.decidedBlindedBlock() {
		_, blkToSign, err = decidedValue.GetBlindedBlockData()
		if err != nil {
			return errors.Wrap(err, "could not get blinded block data")
		}
	} else {
		_, blkToSign, err = decidedValue.GetBlockData()
		if err != nil {
			return errors.Wrap(err, "could not get block data")
		}
	}

	msg, err := r.BaseRunner.signBeaconObject(
		r,
		blkToSign,
		decidedValue.Duty.Slot,
		spectypes.DomainProposer,
	)
	if err != nil {
		return errors.Wrap(err, "failed signing attestation data")
	}
	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     decidedValue.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	postSignedMsg, err := r.BaseRunner.signPostConsensusMsg(r, postConsensusMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign post consensus msg")
	}
	data, err := postSignedMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}
	msgToBroadcast := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   spectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey, r.BaseRunner.BeaconRoleType),
		Data:    data,
	}
	if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil
}

func (r *ProposerRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg *spectypes.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}

	duty := r.GetState().DecidedValue.Duty
	logger = logger.With(fields.Slot(duty.Slot))
	logger.Debug("🧩 got partial signatures",
		zap.Uint64("signer", signedMsg.Signer))

	if !quorum {
		return nil
	}

	r.metrics.EndPostConsensus()

	for _, root := range roots {
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			for _, root := range roots {
				r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root)
			}
			return errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
		}
		specSig := phase0.BLSSignature{}
		copy(specSig[:], sig)

		logger.Debug("🧩 reconstructed partial signatures",
			zap.Uint64s("signers", getPostConsensusSigners(r.GetState(), root)))

		blockSubmissionEnd := r.metrics.StartBeaconSubmission()

		start := time.Now()
		var blk any
		if r.decidedBlindedBlock() {
			vBlindedBlk, _, err := r.GetState().DecidedValue.GetBlindedBlockData()
			if err != nil {
				return errors.Wrap(err, "could not get blinded block")
			}
			blk = vBlindedBlk

			if err := r.GetBeaconNode().SubmitBlindedBeaconBlock(vBlindedBlk, specSig); err != nil {
				r.metrics.RoleSubmissionFailed()
				logger.Error("❌ could not submit to Beacon chain reconstructed signed blinded Beacon block", zap.Error(err))
				return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed blinded Beacon block")
			}
		} else {
			vBlk, _, err := r.GetState().DecidedValue.GetBlockData()
			if err != nil {
				return errors.Wrap(err, "could not get block")
			}
			blk = vBlk

			if err := r.GetBeaconNode().SubmitBeaconBlock(vBlk, specSig); err != nil {
				r.metrics.RoleSubmissionFailed()
				logger.Error("❌ could not submit to Beacon chain reconstructed signed Beacon block", zap.Error(err))
				return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed Beacon block")
			}
		}

		blockSubmissionEnd()
		r.metrics.EndDutyFullFlow(r.GetState().RunningInstance.State.Round)
		r.metrics.RoleSubmitted()

		blockSummary, summarizeErr := summarizeBlock(blk)
		logger.Info("✅ successfully submitted block proposal",
			fields.Slot(signedMsg.Message.Slot),
			fields.Height(r.BaseRunner.QBFTController.Height),
			fields.Round(r.GetState().RunningInstance.State.Round),
			zap.String("block_hash", blockSummary.Hash.String()),
			zap.Bool("blinded", blockSummary.Blinded),
			zap.Duration("took", time.Since(start)),
			zap.NamedError("summarize_err", summarizeErr))
	}
	r.GetState().Finished = true
	return nil
}

// decidedBlindedBlock returns true if decided value has a blinded block, false if regular block
// WARNING!! should be called after decided only
func (r *ProposerRunner) decidedBlindedBlock() bool {
	_, _, err := r.BaseRunner.State.DecidedValue.GetBlindedBlockData()
	return err == nil
}

func (r *ProposerRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	epoch := r.BaseRunner.BeaconNetwork.EstimatedEpochAtSlot(r.GetState().StartingDuty.Slot)
	return []ssz.HashRoot{spectypes.SSZUint64(epoch)}, spectypes.DomainRandao, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *ProposerRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	if r.decidedBlindedBlock() {
		_, data, err := r.GetState().DecidedValue.GetBlindedBlockData()
		if err != nil {
			return nil, phase0.DomainType{}, errors.Wrap(err, "could not get blinded block data")
		}
		return []ssz.HashRoot{data}, spectypes.DomainProposer, nil
	}

	_, data, err := r.GetState().DecidedValue.GetBlockData()
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not get block data")
	}
	return []ssz.HashRoot{data}, spectypes.DomainProposer, nil
}

// executeDuty steps:
// 1) sign a partial randao sig and wait for 2f+1 partial sigs from peers
// 2) reconstruct randao and send GetBeaconBlock to BN
// 3) start consensus on duty + block data
// 4) Once consensus decides, sign partial block and broadcast
// 5) collect 2f+1 partial sigs, reconstruct and broadcast valid block sig to the BN
func (r *ProposerRunner) executeDuty(logger *zap.Logger, duty *spectypes.Duty) error {
	r.metrics.StartDutyFullFlow()
	r.metrics.StartPreConsensus()

	// sign partial randao
	epoch := r.GetBeaconNode().GetBeaconNetwork().EstimatedEpochAtSlot(duty.Slot)
	msg, err := r.BaseRunner.signBeaconObject(r, spectypes.SSZUint64(epoch), duty.Slot, spectypes.DomainRandao)
	if err != nil {
		return errors.Wrap(err, "could not sign randao")
	}
	msgs := spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	// sign msg
	signature, err := r.GetSigner().SignRoot(msgs, spectypes.PartialSignatureType, r.GetShare().SharePubKey)
	if err != nil {
		return errors.Wrap(err, "could not sign randao msg")
	}
	signedPartialMsg := &spectypes.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: signature,
		Signer:    r.GetShare().OperatorID,
	}

	// broadcast
	data, err := signedPartialMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode randao pre-consensus signature msg")
	}
	msgToBroadcast := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   spectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey, r.BaseRunner.BeaconRoleType),
		Data:    data,
	}
	if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial randao sig")
	}

	logger.Debug("🔏 signed & broadcasted partial RANDAO signature")

	return nil
}

func (r *ProposerRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *ProposerRunner) GetNetwork() specssv.Network {
	return r.network
}

func (r *ProposerRunner) GetBeaconNode() specssv.BeaconNode {
	return r.beacon
}

func (r *ProposerRunner) GetShare() *spectypes.Share {
	return r.BaseRunner.Share
}

func (r *ProposerRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *ProposerRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *ProposerRunner) GetSigner() spectypes.KeyManager {
	return r.signer
}

// Encode returns the encoded struct in bytes or error
func (r *ProposerRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *ProposerRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *ProposerRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

// blockSummary contains essentials about a block. Useful for logging.
type blockSummary struct {
	Hash    phase0.Hash32
	Blinded bool
	Version spec.DataVersion
}

// summarizeBlock returns a blockSummary for the given block.
func summarizeBlock(block any) (summary blockSummary, err error) {
	if block == nil {
		return summary, fmt.Errorf("block is nil")
	}
	switch b := block.(type) {
	case *api.VersionedProposal:
		if b.Blinded {
			switch b.Version {
			case spec.DataVersionCapella:
				return summarizeBlock(b.CapellaBlinded)
			case spec.DataVersionDeneb:
				return summarizeBlock(b.DenebBlinded)
			default:
				return summary, fmt.Errorf("unsupported blinded block version %d", b.Version)
			}
		}
		switch b.Version {
		case spec.DataVersionCapella:
			return summarizeBlock(b.Capella)
		case spec.DataVersionDeneb:
			if b.Deneb == nil {
				return summary, fmt.Errorf("deneb block contents is nil")
			}
			return summarizeBlock(b.Deneb.Block)
		default:
			return summary, fmt.Errorf("unsupported block version %d", b.Version)
		}

	case *capella.BeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayload == nil {
			return summary, fmt.Errorf("block, body or execution payload is nil")
		}
		summary.Hash = b.Body.ExecutionPayload.BlockHash
		summary.Version = spec.DataVersionCapella

	case *deneb.BeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayload == nil {
			return summary, fmt.Errorf("block, body or execution payload is nil")
		}
		summary.Hash = b.Body.ExecutionPayload.BlockHash
		summary.Version = spec.DataVersionDeneb

	case *apiv1deneb.BlockContents:
		return summarizeBlock(b.Block)

	case *apiv1capella.BlindedBeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayloadHeader == nil {
			return summary, fmt.Errorf("block, body or execution payload header is nil")
		}
		summary.Hash = b.Body.ExecutionPayloadHeader.BlockHash
		summary.Blinded = true
		summary.Version = spec.DataVersionCapella

	case *apiv1deneb.BlindedBeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayloadHeader == nil {
			return summary, fmt.Errorf("block, body or execution payload header is nil")
		}
		summary.Hash = b.Body.ExecutionPayloadHeader.BlockHash
		summary.Blinded = true
		summary.Version = spec.DataVersionDeneb
	}
	return
}
