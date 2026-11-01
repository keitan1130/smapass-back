// Package attestationfixture creates synthetic certificates for tests only.
// No application DI or runtime policy imports this package.
package attestationfixture

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"slices"
	"sync"
	"time"

	"smapass-back/internal/domain/entity"
)

const PackageName = "test.smapass.app"

var signingDigest = sha256.Sum256([]byte("synthetic APK signing certificate for smapass tests"))

func SigningDigest() string { return hex.EncodeToString(signingDigest[:]) }

type Authority struct {
	Root *x509.Certificate
	Key  *ecdsa.PrivateKey
}

func NewAuthority() (*Authority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(100), Subject: pkix.Name{CommonName: "Synthetic test-only Android root"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	root, err := x509.ParseCertificate(der)
	return &Authority{root, key}, err
}

var defaultAuthority = sync.OnceValues(NewAuthority)

func DefaultAuthority() (*Authority, error) { return defaultAuthority() }

type Options struct {
	PackageName             string
	SigningDigest           []byte
	Level                   int
	Locked                  bool
	BootState               int
	Origin                  int
	AttestedIMEIs           []string
	ChallengeOverride       []byte
	RawExtension            []byte
	DuplicateTag            bool
	NotAfter                time.Time
	IntermediateAttestation bool
}

func DefaultOptions() Options {
	return Options{PackageName: PackageName, SigningDigest: slices.Clone(signingDigest[:]), Level: 1, Locked: true, NotAfter: time.Now().Add(time.Hour)}
}

func marshal(v any) []byte {
	data, err := asn1.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
func explicit(tag int, der []byte) []byte {
	return marshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: tag, IsCompound: true, Bytes: der})
}
func sequence(data []byte) asn1.RawValue {
	return asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSequence, IsCompound: true, Bytes: data}
}
func set(values []int) []byte {
	data, err := asn1.MarshalWithParams(values, "set")
	if err != nil {
		panic(err)
	}
	return data
}

func (a *Authority) Proof(challenge *entity.KeyChallenge, options Options) (string, entity.KeyProof, error) {
	nonce, err := base64.StdEncoding.DecodeString(challenge.AttestationChallenge)
	if err != nil {
		return "", entity.KeyProof{}, err
	}
	payload, err := base64.StdEncoding.DecodeString(challenge.SigningPayload)
	if err != nil {
		return "", entity.KeyProof{}, err
	}
	if options.ChallengeOverride != nil {
		nonce = options.ChallengeOverride
	}
	app := marshal(struct {
		Packages []struct {
			Name    []byte
			Version int64
		} `asn1:"set"`
		Digests [][]byte `asn1:"set"`
	}{[]struct {
		Name    []byte
		Version int64
	}{{[]byte(options.PackageName), 1}}, [][]byte{options.SigningDigest}})
	sw := sequence(explicit(709, marshal(app)))
	var hw []byte
	for _, item := range []struct {
		tag  int
		data []byte
	}{{1, set([]int{2, 3})}, {2, marshal(3)}, {3, marshal(256)}, {5, set([]int{4})}, {10, marshal(1)}, {702, marshal(options.Origin)}} {
		hw = append(hw, explicit(item.tag, item.data)...)
	}
	boot := marshal(struct {
		Key    []byte
		Locked bool
		State  asn1.Enumerated
		Hash   []byte
	}{make([]byte, 32), options.Locked, asn1.Enumerated(options.BootState), make([]byte, 32)})
	hw = append(hw, explicit(704, boot)...)
	hw = append(hw, explicit(705, marshal(150000))...)
	hw = append(hw, explicit(706, marshal(202610))...)
	for i, imei := range options.AttestedIMEIs {
		tag := 714
		if i == 1 {
			tag = 723
		}
		hw = append(hw, explicit(tag, marshal([]byte(imei)))...)
	}
	if options.DuplicateTag {
		hw = append(hw, explicit(702, marshal(options.Origin))...)
	}
	extension := marshal(struct {
		Version            int
		Level              asn1.Enumerated
		KeymasterVersion   int
		KeymasterLevel     asn1.Enumerated
		Challenge          []byte
		Unique             []byte
		Software, Hardware asn1.RawValue
	}{400, asn1.Enumerated(options.Level), 400, asn1.Enumerated(options.Level), nonce, []byte{}, sw, sequence(hw)})
	if options.RawExtension != nil {
		extension = options.RawExtension
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", entity.KeyProof{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return "", entity.KeyProof{}, err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Android Keystore Key (synthetic fixture)"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: options.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtraExtensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11129, 2, 1, 17}, Value: extension}}}
	parent, signer := a.Root, a.Key
	var intermediate []byte
	if options.IntermediateAttestation {
		intermediateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return "", entity.KeyProof{}, err
		}
		ca := *template
		ca.IsCA = true
		ca.BasicConstraintsValid = true
		ca.KeyUsage = x509.KeyUsageCertSign
		ca.SerialNumber = big.NewInt(101)
		intermediate, err = x509.CreateCertificate(rand.Reader, &ca, a.Root, &intermediateKey.PublicKey, a.Key)
		if err != nil {
			return "", entity.KeyProof{}, err
		}
		parent, err = x509.ParseCertificate(intermediate)
		if err != nil {
			return "", entity.KeyProof{}, err
		}
		signer = intermediateKey
	}
	leaf, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, signer)
	if err != nil {
		return "", entity.KeyProof{}, err
	}
	chain := []string{base64.StdEncoding.EncodeToString(leaf)}
	if intermediate != nil {
		chain = append(chain, base64.StdEncoding.EncodeToString(intermediate))
	}
	chain = append(chain, base64.StdEncoding.EncodeToString(a.Root.Raw))
	digest := sha256.Sum256(payload)
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return "", entity.KeyProof{}, err
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", entity.KeyProof{}, err
	}
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	return public, entity.KeyProof{ChallengeID: challenge.ID, CertificateChain: chain, SignatureDER: base64.StdEncoding.EncodeToString(signature)}, nil
}
