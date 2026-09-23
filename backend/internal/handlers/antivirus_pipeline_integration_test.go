package handlers

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// fakeClamd отвечает по протоколу zINSTREAM одной заранее заданной строкой.
// Так проверяются исходы конвейера, не поднимая настоящий антивирус.
func fakeClamd(t *testing.T, reply string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				// Клиент шлёт команду, куски файла и нулевую длину; содержимое
				// нам не нужно — важен только ответ.
				buf := make([]byte, 32<<10)
				conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				for {
					n, err := conn.Read(buf)
					if err != nil {
						break
					}
					if n >= 4 && string(buf[n-4:n]) == "\x00\x00\x00\x00" {
						break
					}
				}
				conn.Write([]byte(reply))
			}()
		}
	}()
	return listener.Addr().String()
}

// STORE-05 на реальной БД: угроза и недоступный сканер — разные исходы.
// Заражённый файл не попадает в хранилище никогда, непроверенный принимается
// только по политике и остаётся недоступным до проверки.
func TestAntivirusPipelineStates(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	user := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}

	newEntry := func() string {
		t.Helper()
		var id string
		if err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES('internship',$1,$2,$3,'fact',$4,'vuz','{}',100000,100000,'average',$5) RETURNING id::text`,
			tenant.partner, tenant.agreement, tenant.company, year, tenant.admin).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	content := append([]byte("%PDF-1.4\n"), []byte("подтверждающий документ")...)

	t.Run("чистый файл принимается и доступен", func(t *testing.T) {
		handlers := AttachmentHandlers{DB: db, UploadDir: t.TempDir(),
			ScannerAddress: fakeClamd(t, "stream: OK\x00")}
		entry := newEntry()
		response := uploadFile(t, handlers, user, entry, "clean.pdf", content)
		if response.Code != 201 {
			t.Fatalf("чистый файл должен приниматься: %d %s", response.Code, response.Body.String())
		}
		var result struct {
			Files []struct {
				ID         string `json:"id"`
				ScanStatus string `json:"scan_status"`
			} `json:"files"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Files[0].ScanStatus != "clean" {
			t.Fatalf("состояние проверки %q, ожидалось clean", result.Files[0].ScanStatus)
		}
		recorder := httptest.NewRecorder()
		handlers.Download(recorder, httptest.NewRequest("GET", "/api/attachments/"+result.Files[0].ID+"/download", nil), user, result.Files[0].ID)
		if recorder.Code != 200 {
			t.Fatalf("чистый файл должен скачиваться: %d", recorder.Code)
		}
	})

	t.Run("заражённый файл не попадает в хранилище", func(t *testing.T) {
		uploadDir := t.TempDir()
		handlers := AttachmentHandlers{DB: db, UploadDir: uploadDir,
			ScannerAddress: fakeClamd(t, "stream: Eicar-Test-Signature FOUND\x00")}
		entry := newEntry()
		response := uploadFile(t, handlers, user, entry, "infected.pdf", content)
		if response.Code != 422 {
			t.Fatalf("заражённый файл должен отклоняться: %d %s", response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), "антивирус") {
			t.Fatalf("отказ должен называть причину: %s", response.Body.String())
		}
		var stored int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM attachments WHERE entry_id::text=$1`, entry).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != 0 {
			t.Fatal("заражённый файл не должен оставлять запись")
		}
	})

	t.Run("недоступный сканер по умолчанию не пропускает файл", func(t *testing.T) {
		// Адрес, на котором никто не слушает: сканер недоступен.
		handlers := AttachmentHandlers{DB: db, UploadDir: t.TempDir(), ScannerAddress: "127.0.0.1:1"}
		entry := newEntry()
		response := uploadFile(t, handlers, user, entry, "unchecked.pdf", content)
		if response.Code != 422 {
			t.Fatalf("без проверки файл не принимается: %d %s", response.Code, response.Body.String())
		}
		var stored int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM attachments WHERE entry_id::text=$1`, entry).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != 0 {
			t.Fatal("непринятый файл не должен оставлять запись")
		}
	})

	t.Run("политика карантина принимает файл, но не выдаёт его", func(t *testing.T) {
		handlers := AttachmentHandlers{DB: db, UploadDir: t.TempDir(),
			ScannerAddress: "127.0.0.1:1", ScannerPolicy: "quarantine"}
		entry := newEntry()
		response := uploadFile(t, handlers, user, entry, "quarantined.pdf", content)
		if response.Code != 201 {
			t.Fatalf("по политике карантина файл принимается: %d %s", response.Code, response.Body.String())
		}
		var result struct {
			Files []struct {
				ID         string `json:"id"`
				ScanStatus string `json:"scan_status"`
			} `json:"files"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Files[0].ScanStatus != "quarantined" {
			t.Fatalf("состояние проверки %q, ожидалось quarantined", result.Files[0].ScanStatus)
		}
		var scannedAt *time.Time
		if err := db.QueryRowContext(ctx, `SELECT scanned_at FROM attachments WHERE id::text=$1`, result.Files[0].ID).Scan(&scannedAt); err != nil {
			t.Fatal(err)
		}
		if scannedAt == nil {
			t.Fatal("у карантина должна быть отметка времени проверки")
		}
		recorder := httptest.NewRecorder()
		handlers.Download(recorder, httptest.NewRequest("GET", "/api/attachments/"+result.Files[0].ID+"/download", nil), user, result.Files[0].ID)
		if recorder.Code != 409 {
			t.Fatalf("файл в карантине не выдаётся: %d %s", recorder.Code, recorder.Body.String())
		}
		// Состояние видно в списке вложений, а не только в базе.
		list := httptest.NewRecorder()
		handlers.ListForEntry(list, httptest.NewRequest("GET", "/api/entries/"+entry+"/attachments", nil), user, entry)
		if !strings.Contains(list.Body.String(), `"scan_status":"quarantined"`) {
			t.Fatalf("состояние проверки должно быть видно в списке: %s", list.Body.String())
		}
	})
}
