package handlers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// QA-07: гонка на исчерпании квоты. Квота проверяется под блокировкой строки
// загружающего, поэтому параллельные загрузки на границе не могут вместе
// превысить лимит: принимается ровно столько файлов, сколько в него помещается,
// а отказанные не оставляют ни записи, ни файла в хранилище.
func TestUploadQuotaHoldsUnderConcurrentUploads(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	user := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}

	const fileSize, fits, attempts = 1024, 3, 60
	uploadDir := t.TempDir()
	handlers := AttachmentHandlers{DB: db, UploadDir: uploadDir, QuotaBytes: fileSize*fits + fileSize/2}

	var entryID string
	if err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
		audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
		VALUES('internship',$1,$2,$3,'fact',$4,'vuz','{}',100000,100000,'average',$5) RETURNING id::text`,
		tenant.partner, tenant.agreement, tenant.company, year, tenant.admin).Scan(&entryID); err != nil {
		t.Fatal(err)
	}

	codes := make([]int, attempts)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Разное содержимое: одинаковые байты хранились бы одним файлом.
			content := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte{byte('a' + i)}, fileSize-9)...)
			<-start
			codes[i] = uploadFile(t, handlers, user, entryID, fmt.Sprintf("f%d.pdf", i), content).Code
		}(i)
	}
	close(start)
	wg.Wait()

	accepted, refused := 0, 0
	for _, code := range codes {
		switch code {
		case 201:
			accepted++
		case 413:
			refused++
		default:
			t.Fatalf("неожиданный код ответа %d (все коды: %v)", code, codes)
		}
	}
	if accepted != fits || refused != attempts-fits {
		t.Fatalf("на границе квоты принято %d, отказано %d; ожидалось %d и %d (коды %v)", accepted, refused, fits, attempts-fits, codes)
	}
	var stored, used int64
	if err := db.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(size_bytes),0) FROM attachments WHERE uploaded_by=$1`, tenant.admin).Scan(&stored, &used); err != nil {
		t.Fatal(err)
	}
	if stored != fits || used > handlers.QuotaBytes {
		t.Fatalf("в базе %d файлов на %d байт при квоте %d", stored, used, handlers.QuotaBytes)
	}
	var files int64
	_ = filepath.Walk(uploadDir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			files++
		}
		return nil
	})
	if files != fits {
		t.Fatalf("в хранилище %d файлов, ожидалось %d: отказанные загрузки не должны оставлять байты", files, fits)
	}
}
