// Package merkle builds the tree whose root is anchored on chain.
//
// Format smapass-merkle-v1:
//   - leaf = SHA-256(0x00 || payload_hash)          payload_hash is the 32-byte SHA-256 of canonical_payload
//   - node = SHA-256(0x01 || left || right)
//   - leaves keep the order given by the caller (leaf_index)
//   - an unpaired last node is promoted to the next level unchanged (never duplicated)
//
// A proof lists the sibling hashes from the leaf upward. Side tells where the
// sibling sits: "left" means node = H(0x01 || sibling || current).
package merkle

import (
	"bytes"
	"crypto/sha256"
	"errors"
)

const Version = "smapass-merkle-v1"

const (
	SideLeft  = "left"
	SideRight = "right"
)

type Step struct {
	Side string
	Hash [32]byte
}

var ErrEmpty = errors.New("merkle: no leaves")

func LeafHash(payloadHash [32]byte) [32]byte {
	return sha256.Sum256(append([]byte{0x00}, payloadHash[:]...))
}

func NodeHash(left, right [32]byte) [32]byte {
	buf := make([]byte, 0, 65)
	buf = append(buf, 0x01)
	buf = append(buf, left[:]...)
	buf = append(buf, right[:]...)
	return sha256.Sum256(buf)
}

// Build returns the root and one proof per payload hash, in input order.
func Build(payloadHashes [][32]byte) ([32]byte, [][]Step, error) {
	if len(payloadHashes) == 0 {
		return [32]byte{}, nil, ErrEmpty
	}
	level := make([][32]byte, len(payloadHashes))
	positions := make([]int, len(payloadHashes))
	proofs := make([][]Step, len(payloadHashes))
	for i, h := range payloadHashes {
		level[i] = LeafHash(h)
		positions[i] = i
		proofs[i] = []Step{}
	}
	for len(level) > 1 {
		for i, pos := range positions {
			switch {
			case pos%2 == 1:
				proofs[i] = append(proofs[i], Step{Side: SideLeft, Hash: level[pos-1]})
			case pos+1 < len(level):
				proofs[i] = append(proofs[i], Step{Side: SideRight, Hash: level[pos+1]})
			}
			positions[i] = pos / 2
		}
		next := make([][32]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			if i+1 < len(level) {
				next = append(next, NodeHash(level[i], level[i+1]))
			} else {
				next = append(next, level[i])
			}
		}
		level = next
	}
	return level[0], proofs, nil
}

// Verify recomputes the root from one payload hash and its proof.
func Verify(payloadHash [32]byte, proof []Step, root [32]byte) bool {
	current := LeafHash(payloadHash)
	for _, step := range proof {
		switch step.Side {
		case SideLeft:
			current = NodeHash(step.Hash, current)
		case SideRight:
			current = NodeHash(current, step.Hash)
		default:
			return false
		}
	}
	return bytes.Equal(current[:], root[:])
}
