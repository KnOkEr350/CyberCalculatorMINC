// Command tlsbootstrap готовит TLS для закрытого контура (OPS-02).
//
//	tlsbootstrap ca      <каталог> <имя центра>                      # локальный центр (ca.pem, ca.key)
//	tlsbootstrap issue   <каталог центра> <каталог TLS> <хост>...    # выпуск и установка серверного сертификата
//	tlsbootstrap import  <fullchain.pem> <privkey.pem> <каталог TLS> <хост> [корни.pem]
//	tlsbootstrap rollback <каталог TLS> <хост> <корни.pem>
//	tlsbootstrap nginx   <хост> <каталог TLS> <адрес приложения>     # конфигурация хост-nginx
//
// Пара устанавливается только после проверки; действующая пара при отказе не
// меняется. Код возврата 3 — проверка пары не пройдена.
package main

import (
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cybercalc/internal/tlsboot"
)

func fail(code int, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "tlsbootstrap: "+format+"\n", args...)
	os.Exit(code)
}

func read(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		fail(1, "%v", err)
	}
	return data
}

func main() {
	if len(os.Args) < 2 {
		fail(2, "использование: tlsbootstrap ca|issue|import|rollback|nginx ...")
	}
	args := os.Args[2:]
	now := time.Now()
	switch os.Args[1] {
	case "ca":
		if len(args) != 2 {
			fail(2, "использование: tlsbootstrap ca <каталог> <имя центра>")
		}
		ca, err := tlsboot.NewCA(args[1], tlsboot.MaxCAValidity, now)
		if err != nil {
			fail(1, "%v", err)
		}
		if err := os.MkdirAll(args[0], 0o700); err != nil {
			fail(1, "%v", err)
		}
		if err := os.WriteFile(filepath.Join(args[0], "ca.pem"), ca.Cert, 0o644); err != nil {
			fail(1, "%v", err)
		}
		if err := os.WriteFile(filepath.Join(args[0], "ca.key"), ca.Key, 0o600); err != nil {
			fail(1, "%v", err)
		}
		fmt.Println("центр создан; ca.pem установите в доверенные на рабочих местах, ca.key храните вне сервера")
	case "issue":
		if len(args) < 3 {
			fail(2, "использование: tlsbootstrap issue <каталог центра> <каталог TLS> <хост>...")
		}
		ca := tlsboot.PEM{Cert: read(filepath.Join(args[0], "ca.pem")), Key: read(filepath.Join(args[0], "ca.key"))}
		pair, err := tlsboot.IssueServer(ca, args[2:], tlsboot.MaxServerValidity, now)
		if err != nil {
			fail(1, "%v", err)
		}
		roots, err := tlsboot.Pool(ca.Cert)
		if err != nil {
			fail(1, "%v", err)
		}
		info, err := tlsboot.Store{Dir: args[1]}.Install(pair, args[2], roots, now)
		if err != nil {
			fail(3, "%v", err)
		}
		fmt.Printf("сертификат установлен: %v, действует до %s\n", info.Hosts, info.NotAfter.Format("2006-01-02"))
	case "import":
		if len(args) < 4 || len(args) > 5 {
			fail(2, "использование: tlsbootstrap import <fullchain.pem> <privkey.pem> <каталог TLS> <хост> [корни.pem]")
		}
		// Без файла корней проверка идёт по системным корням — для сертификата
		// публичного центра; для локального центра корень указывается явно.
		var pool *x509.CertPool
		if len(args) == 5 {
			var err error
			if pool, err = tlsboot.Pool(read(args[4])); err != nil {
				fail(1, "%v", err)
			}
		}
		info, err := tlsboot.Store{Dir: args[2]}.Install(tlsboot.PEM{Cert: read(args[0]), Key: read(args[1])}, args[3], pool, now)
		if err != nil {
			fail(3, "%v", err)
		}
		fmt.Printf("сертификат установлен: %v, действует до %s, цепочка %d\n", info.Hosts, info.NotAfter.Format("2006-01-02"), info.Chain)
	case "rollback":
		if len(args) != 3 {
			fail(2, "использование: tlsbootstrap rollback <каталог TLS> <хост> <корни.pem>")
		}
		pool, err := tlsboot.Pool(read(args[2]))
		if err != nil {
			fail(1, "%v", err)
		}
		info, err := tlsboot.Store{Dir: args[0]}.Rollback(args[1], pool, now)
		if err != nil {
			fail(3, "%v", err)
		}
		fmt.Printf("возвращена предыдущая пара, действует до %s\n", info.NotAfter.Format("2006-01-02"))
	case "nginx":
		if len(args) != 3 {
			fail(2, "использование: tlsbootstrap nginx <хост> <каталог TLS> <адрес приложения>")
		}
		config, err := tlsboot.NginxConfig(args[0], args[1], args[2])
		if err != nil {
			fail(1, "%v", err)
		}
		fmt.Print(config)
	default:
		fail(2, "неизвестная команда %q", os.Args[1])
	}
}
