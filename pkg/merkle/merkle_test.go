package merkle

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func payload(i byte) [32]byte { return sha256.Sum256([]byte{i}) }

func payloads(n int) [][32]byte {
	out := make([][32]byte, n)
	for i := range out {
		out[i] = payload(byte(i))
	}
	return out
}

func TestSingleLeafRootIsLeafHash(t *testing.T) {
	root, proofs, err := Build(payloads(1))
	if err != nil || root != LeafHash(payload(0)) || len(proofs[0]) != 0 {
		t.Fatalf("root=%x proofs=%v err=%v", root, proofs, err)
	}
}

func TestThreeLeavesPromoteUnpairedNode(t *testing.T) {
	p := payloads(3)
	l0, l1, l2 := LeafHash(p[0]), LeafHash(p[1]), LeafHash(p[2])
	want := NodeHash(NodeHash(l0, l1), l2)
	root, proofs, err := Build(p)
	if err != nil || root != want {
		t.Fatalf("root=%x want=%x err=%v", root, want, err)
	}
	if len(proofs[2]) != 1 || proofs[2][0] != (Step{Side: SideLeft, Hash: NodeHash(l0, l1)}) {
		t.Fatalf("promoted leaf proof: %+v", proofs[2])
	}
	if len(proofs[0]) != 2 || proofs[0][0] != (Step{Side: SideRight, Hash: l1}) || proofs[0][1] != (Step{Side: SideRight, Hash: l2}) {
		t.Fatalf("first leaf proof: %+v", proofs[0])
	}
}

func TestDomainSeparationAndDuplicateResistance(t *testing.T) {
	p := payloads(3)
	r3, _, _ := Build(p)
	r4, _, _ := Build(append(p, p[2]))
	if r3 == r4 {
		t.Fatal("duplicating the last leaf must change the root")
	}
	if LeafHash(p[0]) == sha256.Sum256(p[0][:]) {
		t.Fatal("leaf hash must be domain separated")
	}
}

func TestEveryProofVerifies(t *testing.T) {
	for n := 1; n <= 33; n++ {
		p := payloads(n)
		root, proofs, err := Build(p)
		if err != nil {
			t.Fatal(err)
		}
		for i := range p {
			if !Verify(p[i], proofs[i], root) {
				t.Fatalf("n=%d leaf=%d does not verify", n, i)
			}
			if n > 1 && Verify(p[(i+1)%n], proofs[i], root) {
				t.Fatalf("n=%d leaf=%d proof accepted another leaf", n, i)
			}
		}
	}
}

// Fixed vector shared with Web / other implementations. Payload hashes are SHA-256 of
// the single bytes 0x00, 0x01, 0x02.
func TestVector(t *testing.T) {
	root, _, err := Build(payloads(3))
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(root[:]); got != vectorRoot3 {
		t.Fatalf("root changed: %s", got)
	}
}

func TestEmptyAndBadSide(t *testing.T) {
	if _, _, err := Build(nil); err != ErrEmpty {
		t.Fatal("empty input must fail")
	}
	if Verify(payload(0), []Step{{Side: "up"}}, LeafHash(payload(0))) {
		t.Fatal("unknown side must fail")
	}
}

// Computed independently (Python hashlib) from the format described in merkle.go.
const vectorRoot3 = "d1f13800048f5909d4043fc0c152f6643280cba608b672715e56ce159a20629f"
