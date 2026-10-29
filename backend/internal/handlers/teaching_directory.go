package handlers

import (
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

type TeachingDirectoryHandlers struct{ DB *sql.DB }

var fourDigitOKZ = regexp.MustCompile(`^[0-9]{4}$`)

type staffMemberRequest struct {
	FIO                         string    `json:"fio"`
	CompanyPosition             string    `json:"company_position"`
	CompanyDepartment           string    `json:"company_department"`
	OKZCode                     string    `json:"okz_code"`
	ITExperienceDays            int       `json:"it_experience_days"`
	ExperienceDocumentReference string    `json:"experience_document_reference"`
	RecordStatus                string    `json:"record_status"`
	IsTeacher                   *bool     `json:"is_teacher,omitempty"`
	TrainingDirections          *[]string `json:"training_directions,omitempty"`
	PartnerIDs                  *[]string `json:"partner_ids,omitempty"`
}

type teachingPayoutRequest struct {
	TeachingActivityID     string       `json:"teaching_activity_id"`
	TargetQuarter          string       `json:"target_quarter"`
	TargetYear             int          `json:"target_year"`
	PlannedCompensationRub money.Amount `json:"planned_compensation_rub"`
	IsFullyPaid            bool         `json:"is_fully_paid"`
	PayoutDate             string       `json:"payout_date"`
	PayoutOrderNum         string       `json:"payout_order_num"`
	PayoutScanFile         string       `json:"payout_scan_file"`
}

// resolveTeachingStaff turns the typed profile into an immutable snapshot in
// the activity payload. Reports therefore remain readable after a profile is
// updated, while entries.staff_member_id keeps the relational link.
// The department and confirmation flag are server-derived snapshot values that
// are not part of the form schema, so they are returned separately and merged
// only after the payload has passed ValidatePayload.
func (h *EntryHandlers) resolveTeachingStaff(r *http.Request, category, companyID string, payload map[string]interface{}, required bool) (string, map[string]interface{}, error) {
	if category != "teachers" {
		return "", nil, nil
	}
	delete(payload, "employee_department")
	delete(payload, "staff_member_verified")
	staffID := strings.TrimSpace(fmt.Sprint(payload["staff_member_id"]))
	if staffID == "" || staffID == "<nil>" {
		if required {
			return "", nil, errors.New("выберите сотрудника из справочника преподавателей")
		}
		return "", nil, nil
	}
	var fio, position, department, okzCode, document, status string
	var experience int
	err := h.DB.QueryRowContext(r.Context(), `SELECT fio,company_position,company_department,okz_code,it_experience_days,
		experience_document_reference,record_status FROM staff_members WHERE id::text=$1 AND it_company_id::text=$2`, staffID, companyID).
		Scan(&fio, &position, &department, &okzCode, &experience, &document, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, errors.New("сотрудник не найден в справочнике вашей ИТ-компании")
	}
	if err != nil {
		return "", nil, errors.New("не удалось проверить профиль сотрудника")
	}
	payload["staff_member_id"] = staffID
	payload["teacher_full_name"] = fio
	payload["employee_position"] = position
	payload["okz_code"] = okzCode
	payload["it_experience_days"] = experience
	payload["it_experience_reference"] = document
	return staffID, map[string]interface{}{
		"employee_department":   department,
		"staff_member_verified": status == "confirmed",
	}, nil
}

func mergeTeachingSnapshot(payload, snapshot map[string]interface{}) {
	for key, value := range snapshot {
		payload[key] = value
	}
}

func canManageStaffMembers(u middleware.AuthUser) bool {
	if u.EntityType == models.EntityEduInst {
		return false
	}
	switch u.Role {
	case models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin, models.RoleHRSpecialist:
		return true
	default:
		return false
	}
}

func canConfirmStaffMember(u middleware.AuthUser) bool {
	return u.EntityType != models.EntityEduInst && (u.Role == models.RoleSuperAdmin || u.Role == models.RoleHoldingAdmin || u.Role == models.RoleOrgAdmin)
}

func canManageTeachingPayouts(u middleware.AuthUser) bool {
	if u.EntityType == models.EntityEduInst {
		return false
	}
	switch u.Role {
	case models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin, models.RoleFinancialSpecialist:
		return true
	default:
		return false
	}
}

func validateStaffMemberRequest(req *staffMemberRequest) string {
	req.FIO = strings.Join(strings.Fields(req.FIO), " ")
	req.CompanyPosition = strings.Join(strings.Fields(req.CompanyPosition), " ")
	req.CompanyDepartment = strings.TrimSpace(req.CompanyDepartment)
	req.OKZCode = strings.TrimSpace(req.OKZCode)
	req.ExperienceDocumentReference = strings.TrimSpace(req.ExperienceDocumentReference)
	if len([]rune(req.FIO)) < 3 || len([]rune(req.FIO)) > 200 {
		return "ФИО должно содержать от 3 до 200 символов"
	}
	if len([]rune(req.CompanyPosition)) < 2 || len([]rune(req.CompanyPosition)) > 200 {
		return "укажите должность сотрудника"
	}
	if !fourDigitOKZ.MatchString(req.OKZCode) {
		return "код ОКЗ должен состоять из 4 цифр"
	}
	if req.ITExperienceDays < 0 || req.ITExperienceDays > 1827 {
		return "ИТ-стаж должен быть от 0 до 1827 дней за последние 5 лет"
	}
	if req.RecordStatus == "" {
		req.RecordStatus = "unconfirmed_by_admin"
	}
	if req.RecordStatus != "confirmed" && req.RecordStatus != "unconfirmed_by_admin" {
		return "некорректный статус профиля"
	}
	if req.RecordStatus == "confirmed" && (req.ITExperienceDays < 365 || req.ExperienceDocumentReference == "") {
		return "для подтверждения нужен стаж не менее 365 дней и реквизиты подтверждающего документа"
	}
	return ""
}

func normalizeStaffLinks(req *staffMemberRequest) string {
	if req.TrainingDirections != nil {
		seen := map[string]bool{}
		codes := make([]string, 0, len(*req.TrainingDirections))
		for _, code := range *req.TrainingDirections {
			code = strings.TrimSpace(code)
			if !catalogSpecialtyCodePattern.MatchString(code) {
				return "код направления подготовки должен иметь вид 09.03.04"
			}
			if !seen[code] {
				seen[code] = true
				codes = append(codes, code)
			}
		}
		if len(codes) > 50 {
			return "не более 50 направлений подготовки"
		}
		req.TrainingDirections = &codes
	}
	if req.PartnerIDs != nil {
		seen := map[string]bool{}
		ids := make([]string, 0, len(*req.PartnerIDs))
		for _, id := range *req.PartnerIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				return "пустой идентификатор учебного заведения"
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		if len(ids) > 200 {
			return "не более 200 учебных заведений"
		}
		req.PartnerIDs = &ids
	}
	return ""
}

func saveStaffLinks(r *http.Request, tx *sql.Tx, staffID, companyID string, req staffMemberRequest) (string, error) {
	if req.TrainingDirections != nil {
		var known int
		if err := tx.QueryRowContext(r.Context(), `SELECT count(DISTINCT code) FROM active_specialties WHERE code=ANY($1)`,
			pq.Array(*req.TrainingDirections)).Scan(&known); err != nil {
			return "", err
		}
		if known != len(*req.TrainingDirections) {
			return "направление подготовки отсутствует в справочнике приказа № 27", nil
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM staff_member_directions WHERE staff_member_id::text=$1`, staffID); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO staff_member_directions(staff_member_id,specialty_code)
			SELECT $1::uuid,unnest($2::text[])`, staffID, pq.Array(*req.TrainingDirections)); err != nil {
			return "", err
		}
	}
	if req.PartnerIDs != nil {
		var allowed int
		if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM partners WHERE id::text=ANY($1)
			AND it_company_id::text=$2 AND partner_kind IN ('vuz','kolledj')`, pq.Array(*req.PartnerIDs), companyID).Scan(&allowed); err != nil {
			return "", err
		}
		if allowed != len(*req.PartnerIDs) {
			return "преподавателя можно привязать только к вузу или колледжу из ваших партнёров", nil
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM staff_member_partners WHERE staff_member_id::text=$1`, staffID); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO staff_member_partners(staff_member_id,partner_id)
			SELECT $1::uuid,unnest($2::text[])::uuid`, staffID, pq.Array(*req.PartnerIDs)); err != nil {
			return "", err
		}
	}
	return "", nil
}

func (h *TeachingDirectoryHandlers) ListStaffMembers(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canReadTenantData(u) {
		middleware.WriteError(w, http.StatusForbidden, "нет доступа к профилям сотрудников")
		return
	}
	company := itCompanyScope(u)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	teachersOnly := r.URL.Query().Get("teachers_only") == "1"
	includeArchived := r.URL.Query().Get("include_archived") == "1"
	partnerID := strings.TrimSpace(r.URL.Query().Get("partner_id"))
	rows, err := h.DB.QueryContext(r.Context(), `SELECT s.id,s.it_company_id,s.fio,s.company_position,s.company_department,
		s.okz_code,o.name,s.it_experience_days,s.experience_document_reference,s.record_status,s.is_teacher,
		COALESCE(s.archived_at::text,''),s.created_at,s.updated_at,
		ARRAY(SELECT d.specialty_code FROM staff_member_directions d WHERE d.staff_member_id=s.id ORDER BY 1),
		ARRAY(SELECT COALESCE(NULLIF(sp.title,sp.code),'') FROM staff_member_directions d
			LEFT JOIN LATERAL (SELECT code,title FROM active_specialties WHERE code=d.specialty_code LIMIT 1) sp ON true
			WHERE d.staff_member_id=s.id ORDER BY d.specialty_code),
		ARRAY(SELECT sp.partner_id::text FROM staff_member_partners sp JOIN partners p ON p.id=sp.partner_id
			WHERE sp.staff_member_id=s.id ORDER BY p.name),
		ARRAY(SELECT p.name FROM staff_member_partners sp JOIN partners p ON p.id=sp.partner_id
			WHERE sp.staff_member_id=s.id ORDER BY p.name)
		FROM staff_members s JOIN okz_occupations o ON o.version_id=s.okz_version_id AND o.code=s.okz_code
		WHERE ($1='' OR s.it_company_id::text=$1) AND ($2='' OR s.fio ILIKE '%'||$2||'%' OR s.company_position ILIKE '%'||$2||'%')
		AND ($3='' OR s.record_status=$3) AND (NOT $4 OR s.is_teacher) AND ($5 OR s.archived_at IS NULL)
		AND ($6='' OR NOT EXISTS (SELECT 1 FROM staff_member_partners sp WHERE sp.staff_member_id=s.id)
			OR EXISTS (SELECT 1 FROM staff_member_partners sp WHERE sp.staff_member_id=s.id AND sp.partner_id::text=$6))
		ORDER BY s.fio LIMIT 500`, company, query, status, teachersOnly, includeArchived, partnerID)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось загрузить сотрудников")
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, companyID, fio, position, department, code, okzName, document, recordStatus, archivedAt string
		var experience int
		var isTeacher bool
		var createdAt, updatedAt time.Time
		var directions, directionTitles, partnerIDs, partnerNames pq.StringArray
		if err := rows.Scan(&id, &companyID, &fio, &position, &department, &code, &okzName, &experience, &document, &recordStatus,
			&isTeacher, &archivedAt, &createdAt, &updatedAt, &directions, &directionTitles, &partnerIDs, &partnerNames); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "не удалось прочитать сотрудника")
			return
		}
		labels := make([]string, 0, len(directions))
		for index, direction := range directions {
			label := direction
			if index < len(directionTitles) && directionTitles[index] != "" {
				label += " " + directionTitles[index]
			}
			labels = append(labels, label)
		}
		items = append(items, map[string]interface{}{"id": id, "it_company_id": companyID, "fio": fio, "company_position": position,
			"company_department": department, "okz_code": code, "okz_name": okzName, "it_experience_days": experience,
			"experience_document_reference": document, "record_status": recordStatus, "eligible": recordStatus == "confirmed" && experience >= 365,
			"is_teacher": isTeacher, "archived": archivedAt != "", "training_directions": []string(directions), "training_direction_labels": labels,
			"partner_ids": []string(partnerIDs), "partner_names": []string(partnerNames),
			"created_at": createdAt, "updated_at": updatedAt})
	}
	if err := rows.Err(); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось загрузить сотрудников")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, items)
}

func (h *TeachingDirectoryHandlers) CreateStaffMember(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canManageStaffMembers(u) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может изменять профили сотрудников")
		return
	}
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
		return
	}
	var req staffMemberRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if message := validateStaffMemberRequest(&req); message != "" {
		middleware.WriteError(w, http.StatusBadRequest, message)
		return
	}
	if message := normalizeStaffLinks(&req); message != "" {
		middleware.WriteError(w, http.StatusBadRequest, message)
		return
	}
	if req.RecordStatus == "confirmed" && !canConfirmStaffMember(u) {
		middleware.WriteError(w, http.StatusForbidden, "подтвердить профиль может только администратор организации")
		return
	}
	isTeacher := true
	if req.IsTeacher != nil {
		isTeacher = *req.IsTeacher
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO staff_members
		(it_company_id,fio,company_position,company_department,okz_version_id,okz_code,it_experience_days,experience_document_reference,record_status,is_teacher,created_by)
		SELECT $1,$2,$3,$4,v.id,$5,$6,$7,$8,$9,$10 FROM okz_catalog_versions v
		JOIN okz_occupations o ON o.version_id=v.id AND o.code=$5 WHERE v.status='active' RETURNING id`,
		companyID, req.FIO, req.CompanyPosition, req.CompanyDepartment, req.OKZCode, req.ITExperienceDays,
		req.ExperienceDocumentReference, req.RecordStatus, isTeacher, u.ID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		middleware.WriteError(w, http.StatusBadRequest, "код ОКЗ отсутствует в активном справочнике")
		return
	}
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "сотрудник с таким ФИО уже существует или данные не прошли проверку")
		return
	}
	if message, err := saveStaffLinks(r, tx, id, companyID, req); err != nil || message != "" {
		if message == "" {
			message = "не удалось сохранить направления и учебные заведения"
		}
		middleware.WriteError(w, http.StatusBadRequest, message)
		return
	}
	if err := logAudit(r.Context(), tx, "staff_member", id, "create", u.ID, "", nil, req); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *TeachingDirectoryHandlers) UpdateStaffMember(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if !canManageStaffMembers(u) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может изменять профили сотрудников")
		return
	}
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
		return
	}
	var req staffMemberRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if message := validateStaffMemberRequest(&req); message != "" {
		middleware.WriteError(w, http.StatusBadRequest, message)
		return
	}
	if message := normalizeStaffLinks(&req); message != "" {
		middleware.WriteError(w, http.StatusBadRequest, message)
		return
	}
	if req.RecordStatus == "confirmed" && !canConfirmStaffMember(u) {
		middleware.WriteError(w, http.StatusForbidden, "подтвердить профиль может только администратор организации")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE staff_members s SET fio=$1,company_position=$2,company_department=$3,
		okz_version_id=v.id,okz_code=$4,it_experience_days=$5,experience_document_reference=$6,record_status=$7,updated_by=$8,updated_at=now(),
		is_teacher=COALESCE($11,s.is_teacher)
		FROM okz_catalog_versions v JOIN okz_occupations o ON o.version_id=v.id AND o.code=$4
		WHERE s.id::text=$9 AND s.it_company_id::text=$10 AND s.archived_at IS NULL AND v.status='active'`, req.FIO, req.CompanyPosition,
		req.CompanyDepartment, req.OKZCode, req.ITExperienceDays, req.ExperienceDocumentReference, req.RecordStatus, u.ID, id, companyID, req.IsTeacher)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "не удалось сохранить профиль сотрудника")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		middleware.WriteError(w, http.StatusNotFound, "сотрудник или код ОКЗ не найден")
		return
	}
	if message, err := saveStaffLinks(r, tx, id, companyID, req); err != nil || message != "" {
		if message == "" {
			message = "не удалось сохранить направления и учебные заведения"
		}
		middleware.WriteError(w, http.StatusBadRequest, message)
		return
	}
	if err := logAudit(r.Context(), tx, "staff_member", id, "update", u.ID, "", nil, req); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"id": id})
}

// Сотрудник, на которого уже ссылаются мероприятия, не удаляется физически:
// записи хранят ссылку и снимок профиля, поэтому профиль уходит в архив и
// пропадает из списков выбора.
func (h *TeachingDirectoryHandlers) DeleteStaffMember(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if u.Role != models.RoleSuperAdmin || u.EntityType == models.EntityEduInst {
		middleware.WriteError(w, http.StatusForbidden, "удалять преподавателей может только системный администратор")
		return
	}
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var fio string
	var used bool
	err = tx.QueryRowContext(r.Context(), `SELECT s.fio,EXISTS(SELECT 1 FROM entries e WHERE e.staff_member_id=s.id)
		FROM staff_members s WHERE s.id::text=$1 AND s.it_company_id::text=$2 AND s.archived_at IS NULL FOR UPDATE`, id, companyID).Scan(&fio, &used)
	if errors.Is(err, sql.ErrNoRows) {
		middleware.WriteError(w, http.StatusNotFound, "сотрудник не найден")
		return
	}
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось прочитать сотрудника")
		return
	}
	mode := "deleted"
	if used {
		mode = "archived"
		_, err = tx.ExecContext(r.Context(), `UPDATE staff_members SET archived_at=now(),updated_by=$2,updated_at=now() WHERE id::text=$1`, id, u.ID)
	} else {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM staff_members WHERE id::text=$1`, id)
	}
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "не удалось удалить сотрудника")
		return
	}
	if err := logAudit(r.Context(), tx, "staff_member", id, "delete", u.ID, "", map[string]string{"fio": fio}, map[string]string{"mode": mode}); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка удаления")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"id": id, "result": mode})
}

func validateTeachingPayoutRequest(req *teachingPayoutRequest) string {
	req.TeachingActivityID = strings.TrimSpace(req.TeachingActivityID)
	req.TargetQuarter = strings.ToUpper(strings.TrimSpace(req.TargetQuarter))
	req.PayoutDate = strings.TrimSpace(req.PayoutDate)
	req.PayoutOrderNum = strings.TrimSpace(req.PayoutOrderNum)
	req.PayoutScanFile = strings.TrimSpace(req.PayoutScanFile)
	if req.TeachingActivityID == "" {
		return "выберите педагогическую нагрузку"
	}
	if req.TargetQuarter != "Q1" && req.TargetQuarter != "Q2" && req.TargetQuarter != "Q3" && req.TargetQuarter != "Q4" {
		return "квартал должен быть Q1–Q4"
	}
	if req.TargetYear < 2000 || req.TargetYear > 2100 {
		return "некорректный финансовый год"
	}
	if req.PlannedCompensationRub < 0 {
		return "сумма компенсации не может быть отрицательной"
	}
	if req.PayoutDate != "" {
		if _, err := time.Parse("2006-01-02", req.PayoutDate); err != nil {
			return "дата выплаты должна быть в формате ГГГГ-ММ-ДД"
		}
	}
	if req.IsFullyPaid && (req.PayoutDate == "" || req.PayoutOrderNum == "") {
		return "для полной выплаты укажите дату и номер приказа или платёжного документа"
	}
	return ""
}

func (h *TeachingDirectoryHandlers) ListTeachingPayouts(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canReadTenantData(u) {
		middleware.WriteError(w, http.StatusForbidden, "нет доступа к выплатам")
		return
	}
	company := itCompanyScope(u)
	activity := strings.TrimSpace(r.URL.Query().Get("teaching_activity_id"))
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	quarter := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("quarter")))
	partner := partnerScope(u, "")
	rows, err := h.DB.QueryContext(r.Context(), `SELECT p.id,p.teaching_activity_id,p.target_quarter,p.target_year,p.planned_compensation_rub,
		p.is_fully_paid,COALESCE(p.payout_date::text,''),p.payout_order_num,p.payout_scan_file,COALESCE(p.finance_officer_id::text,''),
		e.partner_id,COALESCE(s.fio,e.payload->>'teacher_full_name',''),COALESCE(e.payload->>'course_name',''),p.created_at,p.updated_at
		FROM teaching_payouts p JOIN entries e ON e.id=p.teaching_activity_id LEFT JOIN staff_members s ON s.id=e.staff_member_id
		WHERE ($1='' OR e.it_company_id::text=$1) AND ($2='' OR p.teaching_activity_id::text=$2)
		AND ($3=0 OR p.target_year=$3) AND ($4='' OR p.target_quarter=$4) AND ($5='' OR e.partner_id::text=$5)
		ORDER BY p.target_year DESC,p.target_quarter,e.payload->>'teacher_full_name'`, company, activity, year, quarter, partner)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось загрузить выплаты")
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, entryID, targetQuarter, payoutDate, orderNum, scanFile, officerID, partnerID, teacher, course string
		var targetYear int
		var planned money.Amount
		var paid bool
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &entryID, &targetQuarter, &targetYear, &planned, &paid, &payoutDate, &orderNum, &scanFile, &officerID,
			&partnerID, &teacher, &course, &createdAt, &updatedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "не удалось прочитать выплату")
			return
		}
		items = append(items, map[string]interface{}{"id": id, "teaching_activity_id": entryID, "target_quarter": targetQuarter,
			"target_year": targetYear, "planned_compensation_rub": planned, "is_fully_paid": paid, "payout_date": payoutDate,
			"payout_order_num": orderNum, "payout_scan_file": scanFile, "finance_officer_id": officerID, "partner_id": partnerID,
			"teacher_full_name": teacher, "course_name": course, "created_at": createdAt, "updated_at": updatedAt})
	}
	middleware.WriteJSON(w, http.StatusOK, items)
}

func (h *TeachingDirectoryHandlers) CreateTeachingPayout(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canManageTeachingPayouts(u) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может изменять выплаты")
		return
	}
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
		return
	}
	var req teachingPayoutRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if message := validateTeachingPayoutRequest(&req); message != "" {
		middleware.WriteError(w, http.StatusBadRequest, message)
		return
	}
	var id string
	err := h.DB.QueryRowContext(r.Context(), `INSERT INTO teaching_payouts
		(teaching_activity_id,target_quarter,target_year,planned_compensation_rub,is_fully_paid,payout_date,payout_order_num,payout_scan_file,finance_officer_id,created_by)
		SELECT e.id,$2,$3,$4,$5,NULLIF($6,'')::date,$7,$8,CASE WHEN $5 THEN $9::uuid ELSE NULL END,$9
		FROM entries e WHERE e.id::text=$1 AND e.category_code='teachers' AND e.it_company_id::text=$10 RETURNING id`,
		req.TeachingActivityID, req.TargetQuarter, req.TargetYear, req.PlannedCompensationRub, req.IsFullyPaid,
		req.PayoutDate, req.PayoutOrderNum, req.PayoutScanFile, u.ID, companyID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		middleware.WriteError(w, http.StatusNotFound, "педагогическая нагрузка не найдена")
		return
	}
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "выплата за этот квартал уже существует или данные не прошли проверку")
		return
	}
	_ = logAudit(r.Context(), h.DB, "teaching_payout", id, "create", u.ID, "", nil, req)
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *TeachingDirectoryHandlers) UpdateTeachingPayout(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if !canManageTeachingPayouts(u) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может изменять выплаты")
		return
	}
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
		return
	}
	var req teachingPayoutRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if message := validateTeachingPayoutRequest(&req); message != "" {
		middleware.WriteError(w, http.StatusBadRequest, message)
		return
	}
	result, err := h.DB.ExecContext(r.Context(), `UPDATE teaching_payouts p SET teaching_activity_id=e.id,target_quarter=$1,target_year=$2,
		planned_compensation_rub=$3,is_fully_paid=$4,payout_date=NULLIF($5,'')::date,payout_order_num=$6,payout_scan_file=$7,
		finance_officer_id=CASE WHEN $4 THEN $8::uuid ELSE NULL END,updated_by=$8,updated_at=now()
		FROM entries e WHERE p.id::text=$9 AND e.id::text=$10 AND e.category_code='teachers' AND e.it_company_id::text=$11`,
		req.TargetQuarter, req.TargetYear, req.PlannedCompensationRub, req.IsFullyPaid, req.PayoutDate, req.PayoutOrderNum,
		req.PayoutScanFile, u.ID, id, req.TeachingActivityID, companyID)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "не удалось сохранить выплату")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		middleware.WriteError(w, http.StatusNotFound, "выплата или педагогическая нагрузка не найдена")
		return
	}
	_ = logAudit(r.Context(), h.DB, "teaching_payout", id, "update", u.ID, "", nil, req)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"id": id})
}
