package handlers

import (
	"database/sql"
	"net/http"
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
	Members                    []models.LegalEntityGroupMember `json:"members"`
}

func normalizeLegalEntityGroup(req legalEntityGroupWriteRequest) (legalEntityGroupWriteRequest, time.Time, error) {
	clean := func(value string) string { return strings.Join(strings.Fields(value), " ") }
	req.Name = clean(req.Name)
	req.InteractionAgreementNumber = clean(req.InteractionAgreementNumber)
	req.AuthorizedEntityName = clean(req.AuthorizedEntityName)
	req.AuthorizedEntityINN = strings.TrimSpace(req.AuthorizedEntityINN)
	req.AuthorizedEntityOGRN = strings.TrimSpace(req.AuthorizedEntityOGRN)
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
		authorized_entity_name,authorized_entity_inn,authorized_entity_ogrn,created_at,updated_at
		FROM legal_entity_groups WHERE it_company_id::text=$1 ORDER BY name,id`, companyID)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения групп юридических лиц")
		return
	}
	defer rows.Close()
	out := []models.LegalEntityGroup{}
	for rows.Next() {
		var group models.LegalEntityGroup
		if err = rows.Scan(&group.ID, &group.ITCompanyID, &group.Name, &group.InteractionAgreementNumber, &group.InteractionAgreementDate,
			&group.AuthorizedEntityName, &group.AuthorizedEntityINN, &group.AuthorizedEntityOGRN, &group.CreatedAt, &group.UpdatedAt); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения группы юридических лиц")
			return
		}
		memberRows, memberErr := h.DB.QueryContext(r.Context(), `SELECT id,name,inn,ogrn,is_it_organization,target_amount_rub FROM legal_entity_group_members WHERE group_id=$1 ORDER BY name,id`, group.ID)
		if memberErr != nil {
			middleware.WriteError(w, 500, "ошибка чтения участников группы")
			return
		}
		group.Members = []models.LegalEntityGroupMember{}
		for memberRows.Next() {
			var member models.LegalEntityGroupMember
			var target sql.NullString
			if memberRows.Scan(&member.ID, &member.Name, &member.INN, &member.OGRN, &member.IsITOrganization, &target) != nil {
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
		err = tx.QueryRowContext(r.Context(), `INSERT INTO legal_entity_groups(it_company_id,name,interaction_agreement_number,interaction_agreement_date,authorized_entity_name,authorized_entity_inn,authorized_entity_ogrn,created_by,updated_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING id`, companyID, req.Name, req.InteractionAgreementNumber, agreementDate, req.AuthorizedEntityName, req.AuthorizedEntityINN, req.AuthorizedEntityOGRN, u.ID).Scan(&id)
	} else {
		action = "update"
		result, updateErr := tx.ExecContext(r.Context(), `UPDATE legal_entity_groups SET name=$1,interaction_agreement_number=$2,interaction_agreement_date=$3,authorized_entity_name=$4,authorized_entity_inn=$5,authorized_entity_ogrn=$6,updated_by=$7,updated_at=now() WHERE id::text=$8 AND it_company_id::text=$9`, req.Name, req.InteractionAgreementNumber, agreementDate, req.AuthorizedEntityName, req.AuthorizedEntityINN, req.AuthorizedEntityOGRN, u.ID, id, companyID)
		err = updateErr
		if err == nil {
			if n, _ := result.RowsAffected(); n == 0 {
				middleware.WriteError(w, 404, "группа не найдена")
				return
			}
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `DELETE FROM legal_entity_group_members WHERE group_id=$1`, id)
		}
	}
	if err != nil {
		middleware.WriteError(w, 409, "не удалось сохранить группу: проверьте уникальность названия")
		return
	}
	for _, member := range req.Members {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO legal_entity_group_members(group_id,name,inn,ogrn,is_it_organization,target_amount_rub) VALUES($1,$2,$3,$4,$5,$6)`, id, member.Name, member.INN, member.OGRN, member.IsITOrganization, member.TargetAmountRub); err != nil {
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
