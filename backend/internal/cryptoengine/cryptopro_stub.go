//go:build !cryptopro_cgo

package cryptoengine

import "errors"

func init() {
	RegisterProvider(CryptoProProvider, func() (Engine, error) {
		return nil, errors.Join(ErrProviderUnavailable, errors.New("CryptoPro CSP adapter requires enterprise build with -tags cryptopro_cgo and CGO/PKCS#11 hooks"))
	})
}
