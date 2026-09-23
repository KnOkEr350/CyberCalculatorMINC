package filestore

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// Validate permits supporting documents, images and plain text, not active web
// content, executables or macro-enabled Office files.
func Validate(name, path string, roots ...string) error {
	for _, c := range name {
		if unicode.IsControl(c) {
			return fmt.Errorf("недопустимое имя файла")
		}
	}
	f, err := openForInspection(path, roots)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return err
	}
	buf = buf[:n]
	mime := http.DetectContentType(buf)
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".pdf":
		if bytes.HasPrefix(buf, []byte("%PDF-")) {
			return nil
		}
	case ".png":
		if mime == "image/png" {
			return nil
		}
	case ".jpg", ".jpeg":
		if mime == "image/jpeg" {
			return nil
		}
	case ".txt", ".csv":
		if strings.HasPrefix(mime, "text/plain") {
			return nil
		}
	case ".docx", ".xlsx", ".pptx":
		info, err := f.Stat()
		if err != nil {
			return err
		}
		z, err := zip.NewReader(f, info.Size())
		if err != nil {
			return fmt.Errorf("повреждённый документ Office")
		}
		if len(z.File) > 2000 {
			return fmt.Errorf("слишком сложный документ Office")
		}
		var size uint64
		contentTypes := false
		main := false
		for _, item := range z.File {
			lower := strings.ToLower(item.Name)
			if strings.Contains(lower, "vbaproject") || strings.Contains(lower, "/embeddings/") || item.UncompressedSize64 > 64<<20 {
				return fmt.Errorf("макросы, вложенные объекты и чрезмерно большие части документа запрещены")
			}
			size += item.UncompressedSize64
			if size > 128<<20 {
				return fmt.Errorf("слишком большой распакованный документ")
			}
			contentTypes = contentTypes || item.Name == "[Content_Types].xml"
			main = main || (ext == ".docx" && item.Name == "word/document.xml") || (ext == ".xlsx" && item.Name == "xl/workbook.xml") || (ext == ".pptx" && item.Name == "ppt/presentation.xml")
		}
		if contentTypes && main {
			return nil
		}
	default:
		return fmt.Errorf("разрешены PDF, DOCX, XLSX, PPTX, PNG, JPG, TXT и CSV")
	}
	return fmt.Errorf("содержимое файла не соответствует расширению")
}

// Scan streams to clamd without exposing local filesystem paths. Failure is
// fail-closed when a scanner is configured; infected files are never published.
// STORE-05: вердикт антивируса. Раньше «заражён» и «сканер недоступен» были
// одной ошибкой, и отличить угрозу от сбоя инфраструктуры было нечем — а
// решения по ним разные.
type Verdict string

const (
	// VerdictClean — файл проверен и чист.
	VerdictClean Verdict = "clean"
	// VerdictInfected — сканер нашёл угрозу: файл не сохраняется никогда.
	VerdictInfected Verdict = "infected"
	// VerdictUnavailable — сканер не ответил; решение принимает политика.
	VerdictUnavailable Verdict = "unavailable"
	// VerdictSkipped — сканер не настроен (локальная разработка).
	VerdictSkipped Verdict = "skipped"
)

// ScanResult — вердикт вместе с подписью угрозы, если она названа.
type ScanResult struct {
	Verdict   Verdict
	Signature string
}

// Scan сохранена для вызовов, которым достаточно «прошёл/не прошёл».
func Scan(ctx context.Context, address, path string, roots ...string) error {
	result, err := Inspect(ctx, address, path, roots...)
	if err != nil {
		return err
	}
	if result.Verdict == VerdictInfected {
		return fmt.Errorf("файл не прошёл антивирусную проверку")
	}
	if result.Verdict == VerdictUnavailable {
		return fmt.Errorf("антивирус недоступен")
	}
	return nil
}

// Inspect возвращает вердикт: вызывающий сам решает, что делать с недоступным
// сканером, и отличает это от найденной угрозы.
func Inspect(ctx context.Context, address, path string, roots ...string) (ScanResult, error) {
	if address == "" {
		return ScanResult{Verdict: VerdictSkipped}, nil
	}
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return ScanResult{Verdict: VerdictUnavailable}, nil
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()
	deadline := time.Now().Add(60 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return ScanResult{Verdict: VerdictUnavailable}, nil
	}
	if _, err := io.WriteString(conn, "zINSTREAM\x00"); err != nil {
		return ScanResult{Verdict: VerdictUnavailable}, nil
	}
	f, err := openForInspection(path, roots)
	if err != nil {
		return ScanResult{}, err
	}
	defer f.Close()
	buf := make([]byte, 32<<10)
	var length [4]byte
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			binary.BigEndian.PutUint32(length[:], uint32(n))
			if _, err := conn.Write(length[:]); err != nil {
				return ScanResult{Verdict: VerdictUnavailable}, nil
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return ScanResult{Verdict: VerdictUnavailable}, nil
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return ScanResult{}, readErr
		}
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return ScanResult{Verdict: VerdictUnavailable}, nil
	}
	// zINSTREAM replies end with NUL; clamd may keep the connection open.
	response, err := bufio.NewReader(io.LimitReader(conn, 4097)).ReadString(0)
	if err != nil {
		return ScanResult{Verdict: VerdictUnavailable}, nil
	}
	if len(response) > 4096 {
		return ScanResult{Verdict: VerdictUnavailable}, nil
	}
	if response == "stream: OK\x00" {
		return ScanResult{Verdict: VerdictClean}, nil
	}
	// clamd отвечает «stream: <подпись> FOUND»: имя угрозы попадает в запись
	// о файле, иначе разбирать инцидент будет не по чему.
	signature := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSuffix(response, "\x00"), "stream: "), " FOUND")
	return ScanResult{Verdict: VerdictInfected, Signature: strings.TrimSpace(signature)}, nil
}

func openForInspection(path string, roots []string) (*os.File, error) {
	if len(roots) > 0 {
		return Open(roots[0], path)
	}
	return os.Open(path)
}
