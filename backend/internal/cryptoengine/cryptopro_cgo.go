//go:build cryptopro_cgo

package cryptoengine

import "errors"

func init() {
	RegisterProvider(CryptoProProvider, func() (Engine, error) {
		return nil, errors.Join(ErrProviderUnavailable, errors.New("CryptoPro CSP adapter hook is enabled but enterprise CSP implementation is not linked"))
	})
}
