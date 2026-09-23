// Command tlsbootstrap готовит TLS для закрытого контура (OPS-02).
//
//	tlsbootstrap ca      <каталог> <имя центра>                      # локальный центр (ca.pem, ca.key)
//	tlsbootstrap issue   <каталог центра> <каталог TLS> <хост>...    # выпуск и установка серверного сертификата
//	tlsbootstrap import  <fullchain.pem> <privkey.pem> <каталог TLS> <хост> [корни.pem]
//	tlsbootstrap rollback <каталог TLS> <хост> <корни.pem>
//	tlsbootstrap renew   <каталог центра> <каталог TLS> <дней до конца> <хост>...   # продлить, если нужно
//	tlsbootstrap renew-loop <каталог центра> <каталог TLS> <дней до конца> <период> <хост>...
//	tlsbootstrap status  <каталог TLS> <дней до конца> <хост> [корни.pem]           # код 3 — срок близок
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
	"strconv"
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

func renewOnce(caDir, tlsDir string, before time.Duration, hosts []string) error {
	ca := tlsboot.PEM{Cert: read(filepath.Join(caDir, "ca.pem")), Key: read(filepath.Join(caDir, "ca.key"))}
	renewed, info, err := tlsboot.Store{Dir: tlsDir}.Renew(ca, hosts, before, time.Now())
	if err != nil {
		return err
	}
	if renewed {
		fmt.Printf("сертификат выпущен: %v, действует до %s\n", info.Hosts, info.NotAfter.Format("2006-01-02"))
	} else {
		fmt.Printf("сертификат действует до %s, продление не требуется\n", info.NotAfter.Format("2006-01-02"))
	}
	return nil
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
	case "renew":
		if len(args) < 4 {
			fail(2, "использование: tlsbootstrap renew <каталог центра> <каталог TLS> <дней до конца> <хост>...")
		}
		days, err := strconv.Atoi(args[2])
		if err != nil || days < 1 || days > 365 {
			fail(2, "дней до конца — целое от 1 до 365")
		}
		if err := renewOnce(args[0], args[1], time.Duration(days)*24*time.Hour, args[3:]); err != nil {
			fail(3, "%v", err)
		}
	case "renew-loop":
		if len(args) < 5 {
			fail(2, "использование: tlsbootstrap renew-loop <каталог центра> <каталог TLS> <дней до конца> <период> <хост>...")
		}
		days, err := strconv.Atoi(args[2])
		if err != nil || days < 1 || days > 365 {
			fail(2, "дней до конца — целое от 1 до 365")
		}
		every, err := time.ParseDuration(args[3])
		if err != nil || every < time.Minute {
			fail(2, "период — длительность не короче минуты, например 6h")
		}
		// Сбой одного прохода не должен останавливать продление: следующий
		// проход повторит попытку, а ошибка остаётся в журнале контейнера.
		for {
			if err := renewOnce(args[0], args[1], time.Duration(days)*24*time.Hour, args[4:]); err != nil {
				fmt.Fprintf(os.Stderr, "tlsbootstrap: продление не удалось: %v\n", err)
			}
			time.Sleep(every)
		}
	case "status":
		if len(args) < 3 || len(args) > 4 {
			fail(2, "использование: tlsbootstrap status <каталог TLS> <дней до конца> <хост> [корни.pem]")
		}
		days, err := strconv.Atoi(args[1])
		if err != nil || days < 1 || days > 365 {
			fail(2, "дней до конца — целое от 1 до 365")
		}
		var pool *x509.CertPool
		if len(args) == 4 {
			if pool, err = tlsboot.Pool(read(args[3])); err != nil {
				fail(1, "%v", err)
			}
		}
		info, expiring, err := tlsboot.Store{Dir: args[0]}.Status(args[2], pool, time.Duration(days)*24*time.Hour, now)
		if err != nil {
			fail(3, "%v", err)
		}
		fmt.Printf("действует до %s (осталось %d дн.)\n", info.NotAfter.Format("2006-01-02"), int(info.NotAfter.Sub(now).Hours()/24))
		if expiring {
			fail(3, "до конца срока меньше %d дн.: замените сертификат", days)
		}
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
