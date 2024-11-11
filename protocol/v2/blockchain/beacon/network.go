package beacon

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/network.go -source=./network.go

const (
	defaultSlotDuration                 = 12 * time.Second
	defaultSlotsPerEpoch                = uint64(32)
	defaultEpochsPerSyncCommitteePeriod = uint64(256)
)

type BeaconNetwork interface {
	fmt.Stringer

	ForkVersion() [4]byte
	MinGenesisTime() uint64
	SlotDuration() time.Duration
	SlotsPerEpoch() uint64

	EstimatedCurrentSlot() phase0.Slot
	EstimatedSlotAtTime(time int64) phase0.Slot
	EstimatedTimeAtSlot(slot phase0.Slot) int64
	EstimatedCurrentEpoch() phase0.Epoch
	EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch
	FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot
	EpochStartTime(epoch phase0.Epoch) time.Time

	GetSlotStartTime(slot phase0.Slot) time.Time
	GetSlotEndTime(slot phase0.Slot) time.Time
	IsFirstSlotOfEpoch(slot phase0.Slot) bool
	GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot

	EpochsPerSyncCommitteePeriod() uint64
	EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64
	FirstEpochOfSyncPeriod(period uint64) phase0.Epoch
	LastSlotOfSyncPeriod(period uint64) phase0.Slot

	GetNetwork() Network
	GetBeaconNetwork() spectypes.BeaconNetwork
}

// Network is a beacon chain network.
type Network struct {
	Parent                          spectypes.BeaconNetwork `json:"parent,omitempty" yaml:"Parent,omitempty"`
	Name                            string                  `json:"name,omitempty" yaml:"Name,omitempty"`
	ForkVersionVal                  [4]byte                 `json:"fork_version,omitempty" yaml:"ForkVersion,omitempty"`
	MinGenesisTimeVal               uint64                  `json:"min_genesis_time,omitempty" yaml:"MinGenesisTime,omitempty"`
	SlotDurationVal                 time.Duration           `json:"slot_duration,omitempty" yaml:"SlotDuration,omitempty"`
	SlotsPerEpochVal                uint64                  `json:"slots_per_epoch,omitempty" yaml:"SlotsPerEpoch,omitempty"`
	EpochsPerSyncCommitteePeriodVal uint64                  `json:"epochs_per_sync_committee_period,omitempty" yaml:"EpochsPerSyncCommitteePeriod,omitempty"`
}

// NewNetwork creates a new beacon chain network from a parent spec network.
func NewNetwork(network spectypes.BeaconNetwork) *Network {
	return &Network{
		Parent: network,
	}
}

func (n Network) MarshalYAML() (interface{}, error) {
	forkVersion := ""
	if n.ForkVersionVal != ([4]byte{}) {
		forkVersion = "0x" + hex.EncodeToString(n.ForkVersionVal[:])
	}

	aux := struct {
		Parent                          spectypes.BeaconNetwork `json:"parent,omitempty" yaml:"Parent,omitempty"`
		Name                            string                  `json:"name,omitempty" yaml:"Name,omitempty"`
		ForkVersionVal                  string                  `json:"fork_version,omitempty" yaml:"ForkVersion,omitempty"`
		MinGenesisTimeVal               uint64                  `json:"min_genesis_time,omitempty" yaml:"MinGenesisTime,omitempty"`
		SlotDurationVal                 time.Duration           `json:"slot_duration,omitempty" yaml:"SlotDuration,omitempty"`
		SlotsPerEpochVal                uint64                  `json:"slots_per_epoch,omitempty" yaml:"SlotsPerEpoch,omitempty"`
		EpochsPerSyncCommitteePeriodVal uint64                  `json:"epochs_per_sync_committee_period,omitempty" yaml:"EpochsPerSyncCommitteePeriod,omitempty"`
	}{
		Parent:                          n.Parent,
		Name:                            n.Name,
		ForkVersionVal:                  forkVersion,
		MinGenesisTimeVal:               n.MinGenesisTimeVal,
		SlotDurationVal:                 n.SlotDurationVal,
		SlotsPerEpochVal:                n.SlotsPerEpochVal,
		EpochsPerSyncCommitteePeriodVal: n.EpochsPerSyncCommitteePeriodVal,
	}
	return aux, nil
}

func (n *Network) UnmarshalYAML(unmarshal func(interface{}) error) error {
	aux := struct {
		Parent                          spectypes.BeaconNetwork `json:"parent,omitempty" yaml:"Parent,omitempty"`
		Name                            string                  `json:"name,omitempty" yaml:"Name,omitempty"`
		ForkVersionVal                  string                  `json:"fork_version,omitempty" yaml:"ForkVersion,omitempty"`
		MinGenesisTimeVal               uint64                  `json:"min_genesis_time,omitempty" yaml:"MinGenesisTime,omitempty"`
		SlotDurationVal                 time.Duration           `json:"slot_duration,omitempty" yaml:"SlotDuration,omitempty"`
		SlotsPerEpochVal                uint64                  `json:"slots_per_epoch,omitempty" yaml:"SlotsPerEpoch,omitempty"`
		EpochsPerSyncCommitteePeriodVal uint64                  `json:"epochs_per_sync_committee_period,omitempty" yaml:"EpochsPerSyncCommitteePeriod,omitempty"`
	}{}

	if err := unmarshal(&aux); err != nil {
		return err
	}

	forkVersion, err := hex.DecodeString(strings.TrimPrefix(aux.ForkVersionVal, "0x"))
	if err != nil {
		return fmt.Errorf("decode fork version: %w", err)
	}

	var forkVersionArr [4]byte
	if len(forkVersion) != 0 {
		forkVersionArr = [4]byte(forkVersion)
	}

	*n = Network{
		Parent:                          aux.Parent,
		Name:                            aux.Name,
		ForkVersionVal:                  forkVersionArr,
		MinGenesisTimeVal:               aux.MinGenesisTimeVal,
		SlotDurationVal:                 aux.SlotDurationVal,
		SlotsPerEpochVal:                aux.SlotsPerEpochVal,
		EpochsPerSyncCommitteePeriodVal: aux.EpochsPerSyncCommitteePeriodVal,
	}

	return nil
}

func (n Network) String() string {
	if n.Name != "" {
		return n.Name
	}

	if n.Parent != "" {
		return string(n.Parent)
	}

	return ""
}

func (n Network) ForkVersion() [4]byte {
	if n.ForkVersionVal != ([4]byte{}) {
		return n.ForkVersionVal
	}

	if n.Parent != "" {
		return n.Parent.ForkVersion()
	}

	return [4]byte{}
}

func (n Network) MinGenesisTime() uint64 {
	if n.MinGenesisTimeVal != 0 {
		return n.MinGenesisTimeVal
	}

	if n.Parent != "" {
		return n.Parent.MinGenesisTime()
	}

	return 0
}

func (n Network) SlotDuration() time.Duration {
	if n.SlotDurationVal != 0 {
		return n.SlotDurationVal
	}

	if n.Parent != "" {
		return n.Parent.SlotDurationSec()
	}

	return defaultSlotDuration
}

func (n Network) SlotsPerEpoch() uint64 {
	if n.SlotsPerEpochVal != 0 {
		return n.SlotsPerEpochVal
	}

	if n.Parent != "" {
		return n.Parent.SlotsPerEpoch()
	}

	return defaultSlotsPerEpoch
}

// EpochsPerSyncCommitteePeriod returns the number of epochs per sync committee period.
func (n Network) EpochsPerSyncCommitteePeriod() uint64 {
	if n.EpochsPerSyncCommitteePeriodVal != 0 {
		return n.EpochsPerSyncCommitteePeriodVal
	}

	return defaultEpochsPerSyncCommitteePeriod
}

// GetNetwork returns the network
func (n Network) GetNetwork() Network {
	return n
}

// GetBeaconNetwork returns the beacon network the node is on
func (n Network) GetBeaconNetwork() spectypes.BeaconNetwork {
	return n.Parent
}

// GetSlotStartTime returns the start time for the given slot
func (n Network) GetSlotStartTime(slot phase0.Slot) time.Time {
	timeSinceGenesisStart := int64(uint64(slot) * uint64(n.SlotDuration().Seconds())) // #nosec G115
	start := time.Unix(int64(n.MinGenesisTime())+timeSinceGenesisStart, 0)            // #nosec G115
	return start
}

func (n Network) EstimatedTimeAtSlot(slot phase0.Slot) int64 {
	d := int64(slot) * int64(n.SlotDuration().Seconds()) // #nosec G115
	return int64(n.MinGenesisTime()) + d                 // #nosec G115
}

func (n Network) FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(uint64(epoch) * n.SlotsPerEpoch())
}

func (n Network) EpochStartTime(epoch phase0.Epoch) time.Time {
	firstSlot := n.FirstSlotAtEpoch(epoch)
	t := n.EstimatedTimeAtSlot(firstSlot)
	return time.Unix(t, 0)
}

// GetSlotEndTime returns the end time for the given slot
func (n Network) GetSlotEndTime(slot phase0.Slot) time.Time {
	return n.GetSlotStartTime(slot + 1)
}

// EstimatedCurrentSlot returns the estimation of the current slot
func (n Network) EstimatedCurrentSlot() phase0.Slot {
	return n.EstimatedSlotAtTime(time.Now().Unix())
}

// EstimatedSlotAtTime estimates slot at the given time
func (n Network) EstimatedSlotAtTime(time int64) phase0.Slot {
	genesis := int64(n.MinGenesisTime()) // #nosec G115
	if time < genesis {
		return 0
	}
	return phase0.Slot(uint64(time-genesis) / uint64(n.SlotDuration().Seconds())) // #nosec G115
}

// EstimatedCurrentEpoch estimates the current epoch
// https://github.com/ethereum/eth2.0-specs/blob/dev/specs/phase0/beacon-chain.md#compute_start_slot_at_epoch
func (n Network) EstimatedCurrentEpoch() phase0.Epoch {
	return n.EstimatedEpochAtSlot(n.EstimatedCurrentSlot())
}

// EstimatedEpochAtSlot estimates epoch at the given slot
func (n Network) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(slot / phase0.Slot(n.SlotsPerEpoch()))
}

// IsFirstSlotOfEpoch estimates epoch at the given slot
func (n Network) IsFirstSlotOfEpoch(slot phase0.Slot) bool {
	return uint64(slot)%n.SlotsPerEpoch() == 0
}

// GetEpochFirstSlot returns the beacon node first slot in epoch
func (n Network) GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(uint64(epoch) * n.SlotsPerEpoch())
}

// EstimatedSyncCommitteePeriodAtEpoch estimates the current sync committee period at the given Epoch
func (n Network) EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64 {
	return uint64(epoch) / n.EpochsPerSyncCommitteePeriod()
}

// FirstEpochOfSyncPeriod calculates the first epoch of the given sync period.
func (n Network) FirstEpochOfSyncPeriod(period uint64) phase0.Epoch {
	return phase0.Epoch(period * n.EpochsPerSyncCommitteePeriod())
}

// LastSlotOfSyncPeriod calculates the first epoch of the given sync period.
func (n Network) LastSlotOfSyncPeriod(period uint64) phase0.Slot {
	lastEpoch := n.FirstEpochOfSyncPeriod(period+1) - 1
	// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
	// as it will never be included, hence -1.
	return n.GetEpochFirstSlot(lastEpoch+1) - 2
}
