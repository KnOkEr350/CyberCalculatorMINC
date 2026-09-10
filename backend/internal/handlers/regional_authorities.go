package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type RegionalAuthorityHandlers struct {
	DB *sql.DB
}

type regionalAuthorityWriteRequest struct {
	Name      string `json:"name"`
	Region    string `json:"region"`
	INN       string `json:"inn"`
	OGRN      string `json:"ogrn"`
	Status    string `json:"status"`
	SourceURL string `json:"source_url"`
}

func normalizeRegionalAuthority(req regionalAuthorityWriteRequest) (regionalAuthorityWriteRequest, error) {
	req.Name = strings.Join(strings.Fields(req.Name), " ")
	req.Region = strings.Join(strings.Fields(req.Region), " ")
	req.INN = strings.TrimSpace(req.INN)
	req.OGRN = strings.TrimSpace(req.OGRN)
	req.Status = strings.TrimSpace(req.Status)
	req.SourceURL = strings.TrimSpace(req.SourceURL)
	if len([]rune(req.Name)) < 2 || len([]rune(req.Name)) > 300 {
		return req, fmt.Errorf("наименование РОИВ должно содержать от 2 до 300 символов")
	}
	if len([]rune(req.Region)) < 2 || len([]rune(req.Region)) > 200 {
		return req, fmt.Errorf("укажите субъект Российской Федерации")
	}
	if !validINN(req.INN) || !validOGRN(req.OGRN) {
		return req, fmt.Errorf("укажите корректные ИНН и ОГРН РОИВ")
	}
	if req.Status != "active" && req.Status != "inactive" {
		return req, fmt.Errorf("статус РОИВ должен быть active или inactive")
	}
	if !officialRegistryURL(req.SourceURL) {
		return req, fmt.Errorf("укажите HTTPS-ссылку на официальный сайт РОИВ или государственный реестр")
	}
	return req, nil
}

func (h *RegionalAuthorityHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	scope := partnerScope(u, "")
	rows, err := h.DB.QueryContext(r.Context(), `SELECT ra.id,ra.name,ra.region,ra.inn,ra.ogrn,ra.status,ra.source_url,
		ra.created_at,ra.updated_at,count(DISTINCT ap.partner_id),count(DISTINCT e.id)
		FROM regional_authorities ra
		LEFT JOIN agreements a ON a.regional_authority_id=ra.id AND a.agreement_kind='roiv'
		LEFT JOIN agreement_partners ap ON ap.agreement_id=a.id
		LEFT JOIN entries e ON e.agreement_id=a.id AND e.partner_id=ap.partner_id
		WHERE ($1='' OR ap.partner_id::text=$1)
		GROUP BY ra.id ORDER BY ra.region,ra.name,ra.id`+page, scope)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка запроса справочника РОИВ")
		return
	}
	defer rows.Close()
	out := make([]models.RegionalAuthority, 0)
	for rows.Next() {
		var item models.RegionalAuthority
		if err = rows.Scan(&item.ID, &item.Name, &item.Region, &item.INN, &item.OGRN, &item.Status,
			&item.SourceURL, &item.CreatedAt, &item.UpdatedAt, &item.SchoolsCount, &item.ActivitiesCount); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения справочника РОИВ")
			return
		}
		out = append(out, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения справочника РОИВ")
		return
	}
	writePage(w, r, out)
}

func (h *RegionalAuthorityHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !isStaff(u) {
		middleware.WriteError(w, 403, "справочник РОИВ ведёт сотрудник Киберпротекта")
		return
	}
	var req regionalAuthorityWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	req, err := normalizeRegionalAuthority(req)
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO regional_authorities(name,region,inn,ogrn,status,source_url,created_by,updated_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,$7) RETURNING id`, req.Name, req.Region, req.INN, req.OGRN, req.Status, req.SourceURL, u.ID).Scan(&id)
	if err != nil {
		middleware.WriteError(w, 409, "такой РОИВ уже есть в справочнике")
		return
	}
	if logAudit(tx, "regional_authority", id, "create", u.ID, "", nil, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения РОИВ")
		return
	}
	middleware.WriteJSON(w, 201, map[string]string{"id": id})
}

func (h *RegionalAuthorityHandlers) Update(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if !isStaff(u) {
		middleware.WriteError(w, 403, "справочник РОИВ ведёт сотрудник Киберпротекта")
		return
	}
	var req regionalAuthorityWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	req, err := normalizeRegionalAuthority(req)
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE regional_authorities SET name=$1,region=$2,inn=$3,ogrn=$4,status=$5,
		source_url=$6,updated_by=$7,updated_at=now() WHERE id::text=$8`, req.Name, req.Region, req.INN, req.OGRN, req.Status, req.SourceURL, u.ID, id)
	if err != nil {
		middleware.WriteError(w, 409, "не удалось изменить РОИВ")
		return
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		middleware.WriteError(w, 404, "РОИВ не найден")
		return
	}
	if logAudit(tx, "regional_authority", id, "update", u.ID, "", nil, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения РОИВ")
		return
	}
	middleware.WriteJSON(w, 200, map[string]string{"id": id})
}
