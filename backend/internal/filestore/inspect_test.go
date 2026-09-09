package filestore

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScannerFramingAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		cancel, wantOK bool
	}{
		{"clean persistent connection", "stream: OK\x00", false, true},
		{"infected", "stream: Eicar-Test-Signature FOUND\x00", false, false},
		{"malformed", "OK\x00", false, false},
		{"cancelled", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "document")
			if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan struct{})
			defer func() { <-done }()
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(4 * time.Second))
				command := make([]byte, len("zINSTREAM\x00"))
				if _, err := io.ReadFull(conn, command); err != nil {
					return
				}
				if string(command) != "zINSTREAM\x00" {
					t.Error("wrong scanner command")
					return
				}
				for {
					var size uint32
					if err := binary.Read(conn, binary.BigEndian, &size); err != nil {
						return
					}
					if size == 0 {
						break
					}
					if _, err := io.CopyN(io.Discard, conn, int64(size)); err != nil {
						return
					}
				}
				if tc.cancel {
					cancel()
				} else {
					_, _ = io.WriteString(conn, tc.response)
				}
				// Keep the server side open until the client finishes its scan.
				_, _ = io.Copy(io.Discard, conn)
			}()
			started := time.Now()
			err = Scan(ctx, listener.Addr().String(), path, root)
			if (err == nil) != tc.wantOK {
				t.Fatalf("Scan error = %v", err)
			}
			if time.Since(started) > 2*time.Second {
				t.Fatal("scan waited for EOF or ignored cancellation")
			}
		})
	}
}
