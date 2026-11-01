package attestation

import (
	"encoding/asn1"
	"errors"
)

var errDER = errors.New("invalid Android attestation DER")

type keyDescription struct {
	AttestationVersion       int
	AttestationSecurityLevel asn1.Enumerated
	KeymasterVersion         int
	KeymasterSecurityLevel   asn1.Enumerated
	Challenge                []byte
	UniqueID                 []byte
	Software                 asn1.RawValue
	Hardware                 asn1.RawValue
}
type authorizationList map[int]asn1.RawValue
type rootOfTrust struct {
	VerifiedBootKey   []byte
	DeviceLocked      bool
	VerifiedBootState asn1.Enumerated
	VerifiedBootHash  []byte `asn1:"optional"`
}
type packageInfo struct {
	Name    []byte
	Version int64
}
type applicationID struct {
	Packages []packageInfo `asn1:"set"`
	Digests  [][]byte      `asn1:"set"`
}

func unmarshal(data []byte, out any) error {
	rest, err := asn1.Unmarshal(data, out)
	if err != nil || len(rest) != 0 {
		return errDER
	}
	return nil
}

func parseList(raw asn1.RawValue) (authorizationList, error) {
	if raw.Class != asn1.ClassUniversal || raw.Tag != asn1.TagSequence || !raw.IsCompound {
		return nil, errDER
	}
	result := authorizationList{}
	for data := raw.Bytes; len(data) > 0; {
		var v asn1.RawValue
		rest, err := asn1.Unmarshal(data, &v)
		if err != nil || len(rest) >= len(data) || v.Class != asn1.ClassContextSpecific || !v.IsCompound {
			return nil, errDER
		}
		if _, duplicate := result[v.Tag]; duplicate {
			return nil, errDER
		}
		var inner asn1.RawValue
		if err := unmarshal(v.Bytes, &inner); err != nil {
			return nil, err
		}
		result[v.Tag] = inner
		data = rest
	}
	return result, nil
}
func (l authorizationList) integer(tag int) (int, error) {
	v, ok := l[tag]
	if !ok {
		return 0, errDER
	}
	var value int
	if err := unmarshal(v.FullBytes, &value); err != nil {
		return 0, err
	}
	return value, nil
}
func (l authorizationList) octets(tag int) ([]byte, error) {
	v, ok := l[tag]
	if !ok {
		return nil, errDER
	}
	var value []byte
	if err := unmarshal(v.FullBytes, &value); err != nil {
		return nil, err
	}
	return value, nil
}
func (l authorizationList) integers(tag int) ([]int, error) {
	v, ok := l[tag]
	if !ok || v.Class != asn1.ClassUniversal || v.Tag != asn1.TagSet || !v.IsCompound {
		return nil, errDER
	}
	var values []int
	rest, err := asn1.UnmarshalWithParams(v.FullBytes, &values, "set")
	if err != nil || len(rest) != 0 || len(values) == 0 {
		return nil, errDER
	}
	return values, nil
}
