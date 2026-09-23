package handlers

import (
	"database/sql"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
)

type LegalEntityGroupHandlers struct{ DB *sql.DB }

type legalEntityGroupWriteRequest struct {
	Name                       string                          `json:"name"`
	InteractionAgreementNumber string                          `json:"interaction_agreement_number"`
	InteractionAgreementDate   string                          `json:"interaction_agreement_date"`
	AuthorizedEntityName       string                          `json:"authorized_entity_name"`
	AuthorizedEntityINN        string                          `json:"authorized_entity_inn"`
	AuthorizedEntityOGRN       string                          `json:"authorized_entity_ogrn"`
	DocumentReference          string                          `json:"document_reference,omitempty"`
	Members                    []models.LegalEntityGroupMember `json:"members"`
}

func normalizeLegalEntityGroup(req legalEntityGroupWriteRequest) (legalEntityGroupWriteRequest, time.Time, error) {
	clean := func(value string) string { return strings.Join(strings.Fields(value), " ") }
	req.Name = clean(req.Name)
	req.InteractionAgreementNumber = clean(req.InteractionAgreementNumber)
	req.AuthorizedEntityName = clean(req.AuthorizedEntityName)
	req.AuthorizedEntityINN = strings.TrimSpace(req.AuthorizedEntityINN)
	req.AuthorizedEntityOGRN = strings.TrimSpace(req.AuthorizedEntityOGRN)
	req.DocumentReference = clean(req.DocumentReference)
	if len([]rune(req.DocumentReference)) > 1000 {
		return req, time.Time{}, validationError("реквизиты скана договора не длиннее 1000 символов")
	}
	date, err := time.Parse("2006-01-02", req.InteractionAgreementDate)
	if err != nil || len([]rune(req.Name)) < 2 || len([]rune(req.Name)) > 500 ||
		len([]rune(req.InteractionAgreementNumber)) < 1 || len([]rune(req.InteractionAgreementNumber)) > 100 ||
		len([]rune(req.AuthorizedEntityName)) < 2 || len([]rune(req.AuthorizedEntityName)) > 1000 ||
		!validINN(req.AuthorizedEntityINN) || !validOGRN(req.AuthorizedEntityOGRN) {
		return req, time.Time{}, validationError("проверьте название группы, договор о взаимодействии и реквизиты уполномоченного юридического лица")
	}
	if len(req.Members) == 0 || len(req.Members) > 100 {
		return req, time.Time{}, validationError("укажите от 1 до 100 участников группы")
	}
	seen, hasIT, hasAuthorized := map[string]bool{}, false, false
	for i := range req.Members {
		member := &req.Members[i]
		member.Name = clean(member.Name)
		member.INN = strings.TrimSpace(member.INN)
		member.OGRN = strings.TrimSpace(member.OGRN)
		if len([]rune(member.Name)) < 2 || len([]rune(member.Name)) > 1000 || !validINN(member.INN) || !validOGRN(member.OGRN) || seen[member.INN] {
			return req, time.Time{}, validationError("проверьте название, ИНН/ОГРН и отсутствие дублей участников группы")
		}
		if member.TargetAmountRub != nil && *member.TargetAmountRub <= 0 {
			return req, time.Time{}, validationError("целевой объём участника должен быть положительным")
		}
		if member.SharePercent != nil && (*member.SharePercent <= 25 || *member.SharePercent > 100 || math.Round(*member.SharePercent*100) != *member.SharePercent*100) {
			return req, time.Time{}, validationError("доля участия должна быть больше 25% и не больше 100% (не более двух знаков после запятой)")
		}
		seen[member.INN], hasIT = true, hasIT || member.IsITOrganization
		hasAuthorized = hasAuthorized || member.INN == req.AuthorizedEntityINN
	}
	if !hasIT {
		return req, time.Time{}, validationError("в группе должна быть хотя бы одна ИТ-организация")
	}
	if !hasAuthorized {
		return req, time.Time{}, validationError("уполномоченное юридическое лицо должно быть указано среди участников группы")
	}
	return req, date, nil
}

type validationError string

func (e validationError) Error() string { return string(e) }

func (h *LegalEntityGroupHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canPrepareReports(u) {
		middleware.WriteError(w, http.StatusForbidden, "группы юридических лиц доступны ИТ-организации")
		return
	}
	companyID := itCompanyScope(u)
	if companyID == "" {
		middleware.WriteError(w, http.StatusForbidden, "профилю не назначена ИТ-компания")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id,it_company_id,name,interaction_agreement_number,interaction_agreement_date::text,
		authorized_entity_name,authorized_entity_inn,authorized_entity_ogrn,created_at,updated_at,
		status,COALESCE(terminated_on::text,''),COALESCE(termination_reason,''),COALESCE(document_reference,'')
		FROM legal_entity_groups WHERE it_company_id::text=$1 ORDER BY (status='active') DESC,created_at DESC,id`, companyID)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения групп юридических лиц")
		return
	}
	defer rows.Close()
	out := []models.LegalEntityGroup{}
	for rows.Next() {
		var group models.LegalEntityGroup
		if err = rows.Scan(&group.ID, &group.ITCompanyID, &group.Name, &group.InteractionAgreementNumber, &group.InteractionAgreementDate,
			&group.AuthorizedEntityName, &group.AuthorizedEntityINN, &group.AuthorizedEntityOGRN, &group.CreatedAt, &group.UpdatedAt,
			&group.Status, &group.TerminatedOn, &group.TerminationReason, &group.DocumentReference); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения группы юридических лиц")
			return
		}
		memberRows, memberErr := h.DB.QueryContext(r.Context(), `SELECT id,name,inn,ogrn,is_it_organization,target_amount_rub,share_percent::float8 FROM legal_entity_group_members WHERE group_id=$1 ORDER BY name,id`, group.ID)
		if memberErr != nil {
			middleware.WriteError(w, 500, "ошибка чтения участников группы")
			return
		}
		group.Members = []models.LegalEntityGroupMember{}
		for memberRows.Next() {
			var member models.LegalEntityGroupMember
			var target sql.NullString
			var share sql.NullFloat64
			if memberRows.Scan(&member.ID, &member.Name, &member.INN, &member.OGRN, &member.IsITOrganization, &target, &share) != nil {
				memberRows.Close()
				middleware.WriteError(w, 500, "ошибка чтения участника группы")
				return
			}
			if target.Valid {
				value, parseErr := money.Parse(target.String)
				if parseErr != nil {
					memberRows.Close()
					middleware.WriteError(w, 500, "ошибка суммы участника группы")
					return
				}
				member.TargetAmountRub = &value
			}
			if share.Valid {
				member.SharePercent = &share.Float64
			}
			group.Members = append(group.Members, member)
		}
		memberErr = memberRows.Err()
		memberRows.Close()
		if memberErr != nil {
			middleware.WriteError(w, 500, "ошибка чтения участников группы")
			return
		}
		out = append(out, group)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения групп юридических лиц")
		return
	}
	middleware.WriteJSON(w, 200, out)
}

func (h *LegalEntityGroupHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	h.write(w, r, u, "")
}

func (h *LegalEntityGroupHandlers) Update(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	h.write(w, r, u, id)
}

func (h *LegalEntityGroupHandlers) write(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if !canPrepareReports(u) {
		middleware.WriteError(w, 403, "группы юридических лиц ведёт ИТ-организация")
		return
	}
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
		return
	}
	var raw legalEntityGroupWriteRequest
	if decodeJSON(r, &raw) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	req, agreementDate, err := normalizeLegalEntityGroup(raw)
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
	action := "create"
	if id == "" {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO legal_entity_groups(it_company_id,name,interaction_agreement_number,interaction_agreement_date,authorized_entity_name,authorized_entity_inn,authorized_entity_ogrn,document_reference,created_by,updated_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$9) RETURNING id`, companyID, req.Name, req.InteractionAgreementNumber, agreementDate, req.AuthorizedEntityName, req.AuthorizedEntityINN, req.AuthorizedEntityOGRN, req.DocumentReference, u.ID).Scan(&id)
	} else {
		action = "update"
		result, updateErr := tx.ExecContext(r.Context(), `UPDATE legal_entity_groups SET name=$1,interaction_agreement_number=$2,interaction_agreement_date=$3,authorized_entity_name=$4,authorized_entity_inn=$5,authorized_entity_ogrn=$6,document_reference=NULLIF($10,''),updated_by=$7,updated_at=now() WHERE id::text=$8 AND it_company_id::text=$9 AND status='active'`, req.Name, req.InteractionAgreementNumber, agreementDate, req.AuthorizedEntityName, req.AuthorizedEntityINN, req.AuthorizedEntityOGRN, u.ID, id, companyID, req.DocumentReference)
		err = updateErr
		if err == nil {
			if n, _ := result.RowsAffected(); n == 0 {
				middleware.WriteError(w, 404, "действующая группа не найдена: расторгнутый договор не редактируется")
				return
			}
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `DELETE FROM legal_entity_group_members WHERE group_id=$1`, id)
		}
	}
	if err != nil {
		middleware.WriteError(w, 409, "не удалось сохранить группу: у ИТ-организации уже есть действующий договор о взаимодействии")
		return
	}
	for _, member := range req.Members {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO legal_entity_group_members(group_id,name,inn,ogrn,is_it_organization,target_amount_rub,share_percent) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, member.Name, member.INN, member.OGRN, member.IsITOrganization, member.TargetAmountRub, member.SharePercent); err != nil {
			middleware.WriteError(w, 409, "не удалось сохранить участников группы")
			return
		}
	}
	if err = logAudit(r.Context(), tx, "legal_entity_group", id, action, u.ID, "", nil, req); err != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения группы")
		return
	}
	middleware.WriteJSON(w, map[bool]int{true: 201, false: 200}[action == "create"], map[string]string{"id": id})
}

type terminateLegalEntityGroupRequest struct {
	Reason       string `json:"reason"`
	TerminatedOn string `json:"terminated_on,omitempty"`
}

// Terminate расторгает действующий договор: он остаётся в истории, а на его
// место можно завести новый. Причина обязательна.
func (h *LegalEntityGroupHandlers) Terminate(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	// Расторжение договора группы лиц затрагивает всю ИТ-организацию, а не
	// закреплённого партнёра: его вправе оформить только администрация.
	if !canManageCuratorAssignments(u) {
		middleware.WriteError(w, 403, "договор группы лиц расторгает администрация")
		return
	}
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
		return
	}
	var req terminateLegalEntityGroupRequest
	if decodeJSON(r, &req) != nil || strings.TrimSpace(req.Reason) == "" || len([]rune(req.Reason)) > 2000 {
		middleware.WriteError(w, 400, "укажите причину расторжения (до 2000 символов)")
		return
	}
	day := time.Now().In(businessLocation)
	on := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	if strings.TrimSpace(req.TerminatedOn) != "" {
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(req.TerminatedOn))
		if err != nil || parsed.After(on) {
			middleware.WriteError(w, 400, "дата расторжения: ГГГГ-ММ-ДД, не позже сегодняшней")
			return
		}
		on = parsed
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE legal_entity_groups SET status='terminated',terminated_on=$3::date,termination_reason=$4,updated_by=$5,updated_at=now()
		WHERE id::text=$1 AND it_company_id::text=$2 AND status='active'`, id, companyID, on.Format("2006-01-02"), strings.TrimSpace(req.Reason), u.ID)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка расторжения договора")
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		middleware.WriteError(w, 404, "действующий договор не найден")
		return
	}
	if logAudit(r.Context(), tx, "legal_entity_group", id, "terminate", u.ID, strings.TrimSpace(req.Reason), nil, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, 200, map[string]string{"status": "terminated"})
}

// Limits сверяет индикативные лимиты дочерних организаций с целевой суммой
// группы за год: сумма лимитов не должна превышать норматив (проверка сумм).
func (h *LegalEntityGroupHandlers) Limits(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if !canPrepareReports(u) && !canReadTenantData(u) {
		middleware.WriteError(w, 403, "группы юридических лиц доступны ИТ-организации")
		return
	}
	companyID := itCompanyScope(u)
	if companyID == "" {
		middleware.WriteError(w, 403, "профилю не назначена ИТ-компания")
		return
	}
	year := time.Now().Year()
	if raw := r.URL.Query().Get("report_year"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 2000 || parsed > 2100 {
			middleware.WriteError(w, 400, "некорректный год")
			return
		}
		year = parsed
	}
	var exists bool
	if err := h.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM legal_entity_groups WHERE id::text=$1 AND it_company_id::text=$2)`, id, companyID).Scan(&exists); err != nil || !exists {
		middleware.WriteError(w, 404, "группа не найдена")
		return
	}
	type limitRow struct {
		Name         string        `json:"name"`
		INN          string        `json:"inn"`
		SharePercent *float64      `json:"share_percent,omitempty"`
		LimitRub     *money.Amount `json:"limit_rub,omitempty"`
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT name,inn,share_percent::float8,target_amount_rub::text FROM legal_entity_group_members WHERE group_id::text=$1 ORDER BY name,id`, id)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения участников группы")
		return
	}
	defer rows.Close()
	members := []limitRow{}
	var allocated money.Amount
	for rows.Next() {
		var row limitRow
		var share sql.NullFloat64
		var limit sql.NullString
		if err := rows.Scan(&row.Name, &row.INN, &share, &limit); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения участника группы")
			return
		}
		if share.Valid {
			row.SharePercent = &share.Float64
		}
		if limit.Valid {
			value, err := money.Parse(limit.String)
			if err != nil {
				middleware.WriteError(w, 500, "ошибка суммы участника группы")
				return
			}
			row.LimitRub = &value
			if allocated, err = money.Add(allocated, value); err != nil {
				middleware.WriteError(w, 500, "сумма лимитов вне допустимого предела")
				return
			}
		}
		members = append(members, row)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения участников группы")
		return
	}
	response := map[string]interface{}{"report_year": year, "members": members, "allocated_rub": allocated, "target_amount_rub": nil,
		"remaining_rub": nil, "over_allocated": false}
	var target sql.NullString
	if err := h.DB.QueryRowContext(r.Context(), `SELECT target_amount_rub::text FROM organization_budget_targets WHERE it_company_id::text=$1 AND report_year=$2`, companyID, year).Scan(&target); err != nil && err != sql.ErrNoRows {
		middleware.WriteError(w, 500, "ошибка чтения целевой суммы")
		return
	}
	if target.Valid {
		value, err := money.Parse(target.String)
		if err != nil {
			middleware.WriteError(w, 500, "ошибка целевой суммы")
			return
		}
		response["target_amount_rub"] = value
		if allocated <= value {
			response["remaining_rub"] = value - allocated
		} else {
			response["remaining_rub"] = money.Amount(0)
			response["over_allocated"] = true
			response["over_allocated_rub"] = allocated - value
		}
	}
	middleware.WriteJSON(w, 200, response)
}
