package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// Формат файла резервной копии:
//
//	"CCBK1\n" | salt(16) | итерации PBKDF2 (uint32) | размер куска (uint32) | куски
//
// Каждый кусок — uint32 длины шифртекста и шифртекст AES-256-GCM. Ключ выводится
// из парольной фразы PBKDF2-HMAC-SHA256. Nonce куска — номер куска, поэтому
// перестановка, пропуск и повтор кусков меняют nonce и не проходят проверку.
// В AAD входят заголовок и признак последнего куска: обрезанный файл не имеет
// последнего куска с этим признаком и отвергается, а не читается «до конца».
// Куски небольшие, поэтому копия любого размера шифруется и проверяется в
// ограниченной памяти.
const (
	magic          = "CCBK1\n"
	saltSize       = 16
	defaultChunk   = 1 << 20
	maxChunk       = 8 << 20
	defaultRounds  = 600_000
	minRounds      = 100_000
	maxRounds      = 10_000_000
	headerSize     = len(magic) + saltSize + 4 + 4
	maxCipherChunk = maxChunk + 16
)

var (
	ErrFormat       = errors.New("backup: файл не является резервной копией или повреждён заголовок")
	ErrAuth         = errors.New("backup: неверная парольная фраза или файл изменён")
	ErrTruncated    = errors.New("backup: копия обрезана — нет завершающего куска")
	ErrPassphrase   = errors.New("backup: парольная фраза не короче 12 символов")
	errChunkTooLong = errors.New("backup: недопустимый размер куска")
)

type header struct {
	salt   [saltSize]byte
	rounds uint32
	chunk  uint32
	raw    []byte
}

func (h header) key(passphrase string) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, h.salt[:], int(h.rounds), 32)
}

func newHeader(rounds, chunk int) (header, error) {
	var h header
	if _, err := io.ReadFull(rand.Reader, h.salt[:]); err != nil {
		return h, err
	}
	h.rounds, h.chunk = uint32(rounds), uint32(chunk)
	h.raw = make([]byte, 0, headerSize)
	h.raw = append(h.raw, magic...)
	h.raw = append(h.raw, h.salt[:]...)
	h.raw = binary.BigEndian.AppendUint32(h.raw, h.rounds)
	h.raw = binary.BigEndian.AppendUint32(h.raw, h.chunk)
	return h, nil
}

func readHeader(r io.Reader) (header, error) {
	var h header
	buf := make([]byte, headerSize)
	if _, err := io.ReadFull(r, buf); err != nil {
		return h, ErrFormat
	}
	if string(buf[:len(magic)]) != magic {
		return h, ErrFormat
	}
	copy(h.salt[:], buf[len(magic):len(magic)+saltSize])
	h.rounds = binary.BigEndian.Uint32(buf[len(magic)+saltSize:])
	h.chunk = binary.BigEndian.Uint32(buf[len(magic)+saltSize+4:])
	if h.rounds < minRounds || h.rounds > maxRounds || h.chunk == 0 || h.chunk > maxChunk {
		return h, ErrFormat
	}
	h.raw = buf
	return h, nil
}

func aead(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(counter uint64) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint64(n[4:], counter)
	return n
}

func aad(h header, final bool) []byte {
	out := append([]byte{}, h.raw...)
	if final {
		return append(out, 1)
	}
	return append(out, 0)
}

// sealWriter шифрует поток кусками; Close пишет завершающий кусок.
type sealWriter struct {
	out     io.Writer
	h       header
	gcm     cipher.AEAD
	buf     []byte
	counter uint64
	closed  bool
}

func newSealWriter(out io.Writer, passphrase string, rounds, chunk int) (*sealWriter, error) {
	if utf8.RuneCountInString(passphrase) < 12 {
		return nil, ErrPassphrase
	}
	h, err := newHeader(rounds, chunk)
	if err != nil {
		return nil, err
	}
	key, err := h.key(passphrase)
	if err != nil {
		return nil, err
	}
	gcm, err := aead(key)
	if err != nil {
		return nil, err
	}
	if _, err := out.Write(h.raw); err != nil {
		return nil, err
	}
	return &sealWriter{out: out, h: h, gcm: gcm, buf: make([]byte, 0, chunk)}, nil
}

func (w *sealWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("backup: запись после закрытия")
	}
	written := 0
	for len(p) > 0 {
		room := int(w.h.chunk) - len(w.buf)
		n := min(room, len(p))
		w.buf = append(w.buf, p[:n]...)
		p, written = p[n:], written+n
		// Полный кусок отправляется сразу, только если за ним ещё что-то будет:
		// последний кусок обязан нести признак завершения, поэтому кусок
		// удерживается до следующей записи или Close.
		if len(w.buf) == int(w.h.chunk) && len(p) > 0 {
			if err := w.flush(false); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

func (w *sealWriter) flush(final bool) error {
	sealed := w.gcm.Seal(nil, nonce(w.counter), w.buf, aad(w.h, final))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(sealed)))
	if _, err := w.out.Write(length[:]); err != nil {
		return err
	}
	if _, err := w.out.Write(sealed); err != nil {
		return err
	}
	w.counter++
	w.buf = w.buf[:0]
	return nil
}

func (w *sealWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.flush(true)
}

// openReader расшифровывает поток, проверяя каждый кусок и наличие последнего.
type openReader struct {
	in      io.Reader
	h       header
	gcm     cipher.AEAD
	counter uint64
	plain   []byte
	done    bool
	pending []byte // следующий кусок, прочитанный вперёд, чтобы знать, последний ли текущий
}

func newOpenReader(in io.Reader, passphrase string) (*openReader, error) {
	h, err := readHeader(in)
	if err != nil {
		return nil, err
	}
	key, err := h.key(passphrase)
	if err != nil {
		return nil, err
	}
	gcm, err := aead(key)
	if err != nil {
		return nil, err
	}
	return &openReader{in: in, h: h, gcm: gcm}, nil
}

func (r *openReader) readChunk() ([]byte, error) {
	var length [4]byte
	if _, err := io.ReadFull(r.in, length[:]); err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, ErrTruncated
	}
	n := binary.BigEndian.Uint32(length[:])
	if n < uint32(r.gcm.Overhead()) || n > maxCipherChunk {
		return nil, errChunkTooLong
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r.in, buf); err != nil {
		return nil, ErrTruncated
	}
	return buf, nil
}

func (r *openReader) Read(p []byte) (int, error) {
	for len(r.plain) == 0 {
		if r.done {
			return 0, io.EOF
		}
		current := r.pending
		if current == nil {
			var err error
			if current, err = r.readChunk(); err != nil {
				if err == io.EOF {
					return 0, ErrTruncated
				}
				return 0, err
			}
		}
		next, err := r.readChunk()
		final := err == io.EOF
		if err != nil && !final {
			return 0, err
		}
		r.pending = next
		plain, openErr := r.gcm.Open(nil, nonce(r.counter), current, aad(r.h, final))
		if openErr != nil {
			// Не последний кусок, объявленный последним, и последний без признака
			// различаются только признаком в AAD; обе ситуации — порча или обрезка.
			if !final {
				if _, altErr := r.gcm.Open(nil, nonce(r.counter), current, aad(r.h, true)); altErr == nil {
					return 0, fmt.Errorf("%w: после завершающего куска есть данные", ErrAuth)
				}
			} else if _, altErr := r.gcm.Open(nil, nonce(r.counter), current, aad(r.h, false)); altErr == nil {
				return 0, ErrTruncated
			}
			return 0, ErrAuth
		}
		r.counter++
		r.plain = plain
		r.done = final
	}
	n := copy(p, r.plain)
	r.plain = r.plain[n:]
	return n, nil
}
