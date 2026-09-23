package cryptoengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"
)

type SignaturePolicy struct {
	Now                   time.Time
	RequiredUsage         KeyUsage
	TrustedRootIDs        map[string]bool
	RevokedCertificateIDs map[string]bool
}

func (policy SignaturePolicy) ValidateCertificateChain(chain []CertificateRef) (SignerMetadata, error) {
	if len(chain) == 0 {
		return SignerMetadata{}, ErrUntrustedChain
	}
	now := policy.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	required := policy.RequiredUsage
	if required == "" {
		required = KeyUsageVerify
	}
	for index, cert := range chain {
		if err := cert.ValidatePublicModel(); err != nil {
			return SignerMetadata{}, err
		}
		if cert.Status == CertificateRevoked || policy.RevokedCertificateIDs[cert.ID] {
			return SignerMetadata{}, ErrCertificateRevoked
		}
		if cert.Status != CertificateActive {
			return SignerMetadata{}, fmt.Errorf("%w: %s", ErrCertificateInvalid, cert.Status)
		}
		if now.Before(cert.ValidFrom) || now.After(cert.ValidUntil) {
			return SignerMetadata{}, ErrCertificateExpired
		}
		if index == 0 && !cert.Allows(required) {
			return SignerMetadata{}, ErrKeyUsageDenied
		}
		if index+1 < len(chain) && cert.IssuerID != chain[index+1].ID {
			return SignerMetadata{}, ErrUntrustedChain
		}
	}
	root := chain[len(chain)-1]
	if len(policy.TrustedRootIDs) > 0 && !policy.TrustedRootIDs[root.ID] {
		return SignerMetadata{}, ErrUntrustedChain
	}
	return chain[0].SignerMetadata(), nil
}

func CanonicalJSON(input []byte) ([]byte, error) {
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := writeCanonicalJSON(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeCanonicalJSON(out *bytes.Buffer, value interface{}) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		encoded, _ := json.Marshal(v)
		out.Write(encoded)
	case json.Number:
		if _, err := strconv.ParseFloat(v.String(), 64); err != nil {
			return err
		}
		out.WriteString(v.String())
	case []interface{}:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonicalJSON(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			encodedKey, _ := json.Marshal(key)
			out.Write(encodedKey)
			out.WriteByte(':')
			if err := writeCanonicalJSON(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return err
		}
		out.Write(encoded)
	}
	return nil
}
