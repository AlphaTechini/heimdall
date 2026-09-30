package chain

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Decoded is a log decoded with one of the Heimdall ABIs.
type Decoded struct {
	Name string
	Args map[string]any
	Log  types.Log
}

func decode(a *abi.ABI, l types.Log) (*Decoded, error) {
	if len(l.Topics) == 0 {
		return nil, fmt.Errorf("log has no topics")
	}
	ev, err := a.EventByID(l.Topics[0])
	if err != nil {
		return nil, err
	}
	args := map[string]any{}
	var indexed abi.Arguments
	for _, in := range ev.Inputs {
		if in.Indexed {
			indexed = append(indexed, in)
		}
	}
	if err := abi.ParseTopicsIntoMap(args, indexed, l.Topics[1:]); err != nil {
		return nil, err
	}
	if len(l.Data) > 0 {
		if err := ev.Inputs.NonIndexed().UnpackIntoMap(args, l.Data); err != nil {
			return nil, err
		}
	}
	return &Decoded{Name: ev.Name, Args: args, Log: l}, nil
}

// DecodeGuard decodes a HeimdallGuard event.
func DecodeGuard(l types.Log) (*Decoded, error) { return decode(&GuardABI, l) }

// DecodeFactory decodes a HeimdallGuardFactory event.
func DecodeFactory(l types.Log) (*Decoded, error) { return decode(&FactoryABI, l) }

// Addr returns an address argument.
func (d *Decoded) Addr(k string) common.Address {
	v, _ := d.Args[k].(common.Address)
	return v
}

// Big returns an integer argument.
func (d *Decoded) Big(k string) *big.Int {
	if v, err := ToBig(d.Args[k]); err == nil {
		return v
	}
	return new(big.Int)
}

// Bool returns a bool argument.
func (d *Decoded) Bool(k string) bool { v, _ := d.Args[k].(bool); return v }

// Hash returns a bytes32 argument.
func (d *Decoded) Hash(k string) common.Hash {
	if v, ok := d.Args[k].([32]byte); ok {
		return common.Hash(v)
	}
	return common.Hash{}
}

// Uint8 returns a small integer argument (the position type enum).
func (d *Decoded) Uint8(k string) uint8 { v, _ := d.Args[k].(uint8); return v }

// Topic0 helpers used to build log filters.
func GuardTopic(name string) common.Hash   { return GuardABI.Events[name].ID }
func FactoryTopic(name string) common.Hash { return FactoryABI.Events[name].ID }
