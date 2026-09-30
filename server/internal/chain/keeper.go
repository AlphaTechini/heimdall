package chain

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Keeper holds the keeper signing key. The key is never logged or exposed (specs W9).
type Keeper struct {
	key     *ecdsa.PrivateKey
	Address common.Address
	signer  types.Signer
}

// NewKeeper parses a hex private key (no 0x prefix required).
func NewKeeper(hexKey string, chainID *big.Int) (*Keeper, error) {
	k, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		// do not include the key in the error
		return nil, fmt.Errorf("KEEPER_PRIVATE_KEY is not a valid secp256k1 private key")
	}
	return &Keeper{key: k, Address: crypto.PubkeyToAddress(k.PublicKey), signer: types.LatestSignerForChainID(chainID)}, nil
}

// Sign signs a transaction with the keeper key.
func (k *Keeper) Sign(tx *types.Transaction) (*types.Transaction, error) {
	return types.SignTx(tx, k.signer, k.key)
}

// String never reveals the key.
func (k *Keeper) String() string { return "keeper " + k.Address.Hex() }

// PackExit builds HeimdallGuard.exit(positionType, target, maxAmount, reasonHash) calldata.
func PackExit(positionType uint8, target common.Address, maxAmount *big.Int, reasonHash common.Hash) ([]byte, error) {
	return GuardABI.Pack("exit", positionType, target, maxAmount, reasonHash)
}
