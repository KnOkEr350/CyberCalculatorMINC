package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"

	"github.com/lib/pq"
)

type PartnerStructureHandlers struct {
	DB *sql.DB
}

type orgUnitWriteRequest struct {
	ParentUnitID    string `json:"parent_unit_id"`
	UnitLevelType   string `json:"unit_level_type"`
	UnitName        string `json:"unit_name"`
	HeadFIO         string `json:"head_fio"`
	HeadPosition    string `json:"head_position"`
	HeadContacts    string `json:"head_contacts"`
	ChairFIO        string `json:"chair_fio"`
	ChairContacts   string `json:"chair_contacts"`
	CuratorFIO      string `json:"curator_fio"`
	CuratorContacts string `json:"curator_contacts"`
}

type academicGroupWriteRequest struct {
	UnitID          string `json:"unit_id"`
	GroupName       string `json:"group_name"`
	EducationLevel  string `json:"education_level"`
	CourseNum       int    `json:"course_num"`
	CurrentSemester int    `json:"current_semester"`
	SemesterPeriod  string `json:"semester_period"`
	SpecialtyCode   string `json:"specialty_code"`
	StudentsCount   int    `json:"students_count"`
}

var specialtyCodePattern = regexp.MustCompile(`^[0-9]{2}\.[0-9]{2}\.[0-9]{2}$`)

func normalizeShortField(value, label string, max int, required bool) (string, error) {
	value = strings.Join(strings.Fields(value), " ")
	length := utf8.RuneCountInString(value)
	if required && length == 0 {
		return "", fmt.Errorf("поле «%s» обязательно", label)
	}
	if length > max {
		return "", fmt.Errorf("поле «%s» не должно превышать %d символов", label, max)
	}
	return value, nil
}

func normalizeOrgUnit(req *orgUnitWriteRequest) error {
	req.ParentUnitID = strings.TrimSpace(req.ParentUnitID)
	req.UnitLevelType = strings.TrimSpace(req.UnitLevelType)
	allowed := map[string]bool{"institute": true, "faculty": true, "division": true, "department": true, "laboratory": true}
	if !allowed[req.UnitLevelType] {
		return errors.New("выберите допустимый тип подразделения")
	}
	fields := []struct {
		value    *string
		label    string
		max      int
		required bool
	}{
		{&req.UnitName, "Наименование подразделения", 300, true},
		{&req.HeadFIO, "Руководитель", 200, false},
		{&req.HeadPosition, "Должность руководителя", 200, false},
		{&req.HeadContacts, "Контакты руководителя", 500, false},
		{&req.ChairFIO, "Заведующий кафедрой", 200, false},
		{&req.ChairContacts, "Контакты заведующего", 500, false},
		{&req.CuratorFIO, "Куратор подразделения", 200, false},
		{&req.CuratorContacts, "Контакты куратора", 500, false},
	}
	for _, field := range fields {
		value, err := normalizeShortField(*field.value, field.label, field.max, field.required)
		if err != nil {
			return err
		}
		*field.value = value
	}
	if utf8.RuneCountInString(req.UnitName) < 2 {
		return errors.New("наименование подразделения должно содержать не менее 2 символов")
	}
	return nil
}

func normalizeAcademicGroup(req *academicGroupWriteRequest) error {
	req.UnitID = strings.TrimSpace(req.UnitID)
	if req.UnitID == "" {
		return errors.New("выберите структурное подразделение")
	}
	var err error
	if req.GroupName, err = normalizeShortField(req.GroupName, "Шифр группы", 100, true); err != nil {
		return err
	}
	req.SpecialtyCode = strings.TrimSpace(req.SpecialtyCode)
	if !specialtyCodePattern.MatchString(req.SpecialtyCode) {
		return errors.New("код специальности должен иметь формат 09.03.01")
	}
	semesterLimits := map[string][2]int{
		"vo_bachelor": {1, 8}, "vo_master": {9, 12}, "vo_specialist": {1, 13}, "spo": {1, 10},
	}
	limits, ok := semesterLimits[req.EducationLevel]
	if !ok {
		return errors.New("выберите уровень образования")
	}
	if req.CourseNum < 1 || req.CourseNum > 7 {
		return errors.New("курс должен быть от 1 до 7")
	}
	if req.CurrentSemester < limits[0] || req.CurrentSemester > limits[1] {
		return fmt.Errorf("для выбранного уровня допустим семестр от %d до %d", limits[0], limits[1])
	}
	if req.SemesterPeriod != "spring" && req.SemesterPeriod != "autumn" {
		return errors.New("выберите период семестра")
	}
	if req.StudentsCount < 0 || req.StudentsCount > 10000 {
		return errors.New("численность группы должна быть от 0 до 10000")
	}
	return nil
}

func requireStructureWrite(w http.ResponseWriter, r *http.Request, db *sql.DB, u middleware.AuthUser, partnerID string) bool {
	if !canManagePartnerStructure(u) {
		middleware.WriteError(w, http.StatusForbidden, "нет права изменять структуру учебного заведения")
		return false
	}
	return requirePartnerTenant(w, r, db, u, partnerID)
}

func writeStructureDBError(w http.ResponseWriter, err error) {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "exceeds 10 levels"):
		middleware.WriteError(w, http.StatusConflict, "иерархия подразделений не может превышать 10 уровней")
	case strings.Contains(message, "cycle") || strings.Contains(message, "own parent"):
		middleware.WriteError(w, http.StatusConflict, "нельзя создать цикл в иерархии подразделений")
	case strings.Contains(message, "another partner"):
		middleware.WriteError(w, http.StatusConflict, "родительское подразделение относится к другому партнёру")
	case strings.Contains(message, "specialty code"):
		middleware.WriteError(w, http.StatusConflict, "код специальности отсутствует в перечне программ партнёра")
	case strings.Contains(message, "college group") || strings.Contains(message, "hei group") || strings.Contains(message, "only for hei"):
		middleware.WriteError(w, http.StatusConflict, "уровень образования не соответствует виду партнёра")
	case strings.Contains(message, "duplicate key"):
		middleware.WriteError(w, http.StatusConflict, "подразделение или группа с таким наименованием уже существует")
	case strings.Contains(message, "foreign key"):
		middleware.WriteError(w, http.StatusConflict, "запись используется в структуре и не может быть удалена")
	default:
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось сохранить академическую структуру")
	}
}

func (h *PartnerStructureHandlers) ListOrgUnits(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, partnerID string) {
	if !requirePartnerTenant(w, r, h.DB, u, partnerID) {
		return
	}
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `WITH RECURSIVE tree AS (
		SELECT u.id,u.partner_id,u.parent_unit_id,u.unit_level_type,u.unit_name,u.head_fio,u.head_position,u.head_contacts,
			u.chair_fio,u.chair_contacts,u.curator_fio,u.curator_contacts,u.created_at,u.updated_at,1 AS depth,
			ARRAY[lower(u.unit_name)||':'||u.id::text] AS sort_path
		FROM org_units u WHERE u.partner_id::text=$1 AND u.parent_unit_id IS NULL
		UNION ALL
		SELECT u.id,u.partner_id,u.parent_unit_id,u.unit_level_type,u.unit_name,u.head_fio,u.head_position,u.head_contacts,
			u.chair_fio,u.chair_contacts,u.curator_fio,u.curator_contacts,u.created_at,u.updated_at,parent.depth+1,
			parent.sort_path||(lower(u.unit_name)||':'||u.id::text)
		FROM org_units u JOIN tree parent ON parent.id=u.parent_unit_id WHERE parent.depth<10
	)
	SELECT tree.id,tree.partner_id,COALESCE(tree.parent_unit_id::text,''),COALESCE(parent.unit_name,''),tree.depth,
		tree.unit_level_type,tree.unit_name,tree.head_fio,tree.head_position,tree.head_contacts,tree.chair_fio,tree.chair_contacts,
		tree.curator_fio,tree.curator_contacts,tree.created_at,tree.updated_at
	FROM tree LEFT JOIN org_units parent ON parent.id=tree.parent_unit_id ORDER BY tree.sort_path`+page, partnerID)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать структуру")
		return
	}
	defer rows.Close()
	items := []models.OrgUnit{}
	for rows.Next() {
		var item models.OrgUnit
		if err := rows.Scan(&item.ID, &item.PartnerID, &item.ParentUnitID, &item.ParentUnitName, &item.Depth,
			&item.UnitLevelType, &item.UnitName, &item.HeadFIO, &item.HeadPosition, &item.HeadContacts,
			&item.ChairFIO, &item.ChairContacts, &item.CuratorFIO, &item.CuratorContacts, &item.CreatedAt, &item.UpdatedAt); err != nil {
			middleware.WriteError(w, 500, "не удалось прочитать структуру")
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "не удалось прочитать структуру")
		return
	}
	writePage(w, r, items)
}

func (h *PartnerStructureHandlers) CreateOrgUnit(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, partnerID string) {
	if !requireStructureWrite(w, r, h.DB, u, partnerID) {
		return
	}
	var req orgUnitWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	if err := normalizeOrgUnit(&req); err != nil {
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
	err = tx.QueryRowContext(r.Context(), `INSERT INTO org_units(partner_id,parent_unit_id,unit_level_type,unit_name,head_fio,head_position,head_contacts,chair_fio,chair_contacts,curator_fio,curator_contacts)
		VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`, partnerID, req.ParentUnitID, req.UnitLevelType,
		req.UnitName, req.HeadFIO, req.HeadPosition, req.HeadContacts, req.ChairFIO, req.ChairContacts, req.CuratorFIO, req.CuratorContacts).Scan(&id)
	if err != nil {
		writeStructureDBError(w, err)
		return
	}
	if err := logAudit(r.Context(), tx, "org_unit", id, "create", u.ID, "", nil, req); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, 201, map[string]string{"id": id})
}

func loadOrgUnit(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}, id string) (models.OrgUnit, error) {
	var item models.OrgUnit
	err := q.QueryRowContext(ctx, `SELECT id,partner_id,COALESCE(parent_unit_id::text,''),unit_level_type,unit_name,head_fio,head_position,head_contacts,chair_fio,chair_contacts,curator_fio,curator_contacts,created_at,updated_at FROM org_units WHERE id::text=$1`, id).
		Scan(&item.ID, &item.PartnerID, &item.ParentUnitID, &item.UnitLevelType, &item.UnitName, &item.HeadFIO, &item.HeadPosition,
			&item.HeadContacts, &item.ChairFIO, &item.ChairContacts, &item.CuratorFIO, &item.CuratorContacts, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (h *PartnerStructureHandlers) UpdateOrgUnit(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	old, err := loadOrgUnit(r.Context(), h.DB, id)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "подразделение не найдено")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать подразделение")
		return
	}
	if !requireStructureWrite(w, r, h.DB, u, old.PartnerID) {
		return
	}
	var req orgUnitWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	if err := normalizeOrgUnit(&req); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE org_units SET parent_unit_id=NULLIF($2,'')::uuid,unit_level_type=$3,unit_name=$4,
		head_fio=$5,head_position=$6,head_contacts=$7,chair_fio=$8,chair_contacts=$9,curator_fio=$10,curator_contacts=$11,updated_at=now()
		WHERE id::text=$1`, id, req.ParentUnitID, req.UnitLevelType, req.UnitName, req.HeadFIO, req.HeadPosition, req.HeadContacts,
		req.ChairFIO, req.ChairContacts, req.CuratorFIO, req.CuratorContacts)
	if err != nil {
		writeStructureDBError(w, err)
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		middleware.WriteError(w, 404, "подразделение не найдено")
		return
	}
	if err := logAudit(r.Context(), tx, "org_unit", id, "update", u.ID, "", old, req); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, 200, map[string]bool{"ok": true})
}

func (h *PartnerStructureHandlers) DeleteOrgUnit(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	old, err := loadOrgUnit(r.Context(), h.DB, id)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "подразделение не найдено")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать подразделение")
		return
	}
	if !requireStructureWrite(w, r, h.DB, u, old.PartnerID) {
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM org_units WHERE id::text=$1`, id); err != nil {
		writeStructureDBError(w, err)
		return
	}
	if err := logAudit(r.Context(), tx, "org_unit", id, "delete", u.ID, "", old, nil); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка удаления")
		return
	}
	middleware.WriteJSON(w, 200, map[string]bool{"ok": true})
}

func (h *PartnerStructureHandlers) SpecialtyCodes(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, partnerID string) {
	if !requirePartnerTenant(w, r, h.DB, u, partnerID) {
		return
	}
	var codes pq.StringArray
	err := h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(d.program_codes,'{}'::text[]) FROM partners p LEFT JOIN education_directory d ON d.id=p.directory_id WHERE p.id::text=$1`, partnerID).Scan(&codes)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "партнёр не найден")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "не удалось получить специальности")
		return
	}
	middleware.WriteJSON(w, 200, []string(codes))
}

func (h *PartnerStructureHandlers) ListAcademicGroups(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, partnerID string) {
	if !requirePartnerTenant(w, r, h.DB, u, partnerID) {
		return
	}
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT g.id,g.partner_id,g.unit_id,u.unit_name,g.group_name,g.education_level,g.course_num,
		g.current_semester,g.semester_period,g.specialty_code,g.students_count,g.created_at,g.updated_at
		FROM academic_groups g JOIN org_units u ON u.id=g.unit_id WHERE g.partner_id::text=$1 ORDER BY g.group_name,g.id`+page, partnerID)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать академические группы")
		return
	}
	defer rows.Close()
	items := []models.AcademicGroup{}
	for rows.Next() {
		var item models.AcademicGroup
		if err := rows.Scan(&item.ID, &item.PartnerID, &item.UnitID, &item.UnitName, &item.GroupName, &item.EducationLevel,
			&item.CourseNum, &item.CurrentSemester, &item.SemesterPeriod, &item.SpecialtyCode, &item.StudentsCount,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			middleware.WriteError(w, 500, "не удалось прочитать академические группы")
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "не удалось прочитать академические группы")
		return
	}
	writePage(w, r, items)
}

func validateGroupPartner(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}, partnerID string, req academicGroupWriteRequest) error {
	var kind string
	var knownCodes pq.StringArray
	var unitExists bool
	if err := q.QueryRowContext(ctx, `SELECT p.partner_kind,COALESCE(d.program_codes,'{}'::text[]),
		EXISTS(SELECT 1 FROM org_units u WHERE u.id::text=$2 AND u.partner_id=p.id)
		FROM partners p LEFT JOIN education_directory d ON d.id=p.directory_id WHERE p.id::text=$1`, partnerID, req.UnitID).
		Scan(&kind, &knownCodes, &unitExists); err != nil {
		return errors.New("партнёр не найден")
	}
	if !unitExists {
		return errors.New("выбранное подразделение не относится к партнёру")
	}
	if kind == "school" || (kind == "kolledj" && req.EducationLevel != "spo") || (kind == "vuz" && req.EducationLevel == "spo") {
		return errors.New("уровень образования не соответствует виду партнёра")
	}
	if len(knownCodes) > 0 {
		found := false
		for _, code := range knownCodes {
			if code == req.SpecialtyCode {
				found = true
				break
			}
		}
		if !found {
			return errors.New("код специальности отсутствует в перечне программ партнёра")
		}
	}
	return nil
}

func (h *PartnerStructureHandlers) CreateAcademicGroup(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, partnerID string) {
	if !requireStructureWrite(w, r, h.DB, u, partnerID) {
		return
	}
	var req academicGroupWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	if err := normalizeAcademicGroup(&req); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	if err := validateGroupPartner(r.Context(), h.DB, partnerID, req); err != nil {
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
	err = tx.QueryRowContext(r.Context(), `INSERT INTO academic_groups(partner_id,unit_id,group_name,education_level,course_num,current_semester,semester_period,specialty_code,students_count)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, partnerID, req.UnitID, req.GroupName, req.EducationLevel,
		req.CourseNum, req.CurrentSemester, req.SemesterPeriod, req.SpecialtyCode, req.StudentsCount).Scan(&id)
	if err != nil {
		writeStructureDBError(w, err)
		return
	}
	if err := logAudit(r.Context(), tx, "academic_group", id, "create", u.ID, "", nil, req); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, 201, map[string]string{"id": id})
}

func loadAcademicGroup(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}, id string) (models.AcademicGroup, error) {
	var item models.AcademicGroup
	err := q.QueryRowContext(ctx, `SELECT id,partner_id,unit_id,group_name,education_level,course_num,current_semester,semester_period,specialty_code,students_count,created_at,updated_at FROM academic_groups WHERE id::text=$1`, id).
		Scan(&item.ID, &item.PartnerID, &item.UnitID, &item.GroupName, &item.EducationLevel, &item.CourseNum, &item.CurrentSemester,
			&item.SemesterPeriod, &item.SpecialtyCode, &item.StudentsCount, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (h *PartnerStructureHandlers) UpdateAcademicGroup(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	old, err := loadAcademicGroup(r.Context(), h.DB, id)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "академическая группа не найдена")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать академическую группу")
		return
	}
	if !requireStructureWrite(w, r, h.DB, u, old.PartnerID) {
		return
	}
	var req academicGroupWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	if err := normalizeAcademicGroup(&req); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	if err := validateGroupPartner(r.Context(), h.DB, old.PartnerID, req); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `UPDATE academic_groups SET unit_id=$2,group_name=$3,education_level=$4,course_num=$5,
		current_semester=$6,semester_period=$7,specialty_code=$8,students_count=$9,updated_at=now() WHERE id::text=$1`,
		id, req.UnitID, req.GroupName, req.EducationLevel, req.CourseNum, req.CurrentSemester, req.SemesterPeriod, req.SpecialtyCode, req.StudentsCount)
	if err != nil {
		writeStructureDBError(w, err)
		return
	}
	if err := logAudit(r.Context(), tx, "academic_group", id, "update", u.ID, "", old, req); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, 200, map[string]bool{"ok": true})
}

func (h *PartnerStructureHandlers) DeleteAcademicGroup(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	old, err := loadAcademicGroup(r.Context(), h.DB, id)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "академическая группа не найдена")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать академическую группу")
		return
	}
	if !requireStructureWrite(w, r, h.DB, u, old.PartnerID) {
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM academic_groups WHERE id::text=$1`, id); err != nil {
		writeStructureDBError(w, err)
		return
	}
	if err := logAudit(r.Context(), tx, "academic_group", id, "delete", u.ID, "", old, nil); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка удаления")
		return
	}
	middleware.WriteJSON(w, 200, map[string]bool{"ok": true})
}
