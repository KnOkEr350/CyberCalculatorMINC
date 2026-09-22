// Package testfixtures provides shared, tenant-safe PostgreSQL fixtures for
// backend packages and the external cybercalc/tests module.
package testfixtures

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"cybercalc/internal/models"
	"github.com/lib/pq"
)

const DefaultPassword = "FixturePass1!"

var unsafeNamespace = regexp.MustCompile(`[^a-z0-9]+`)
var factorySequence atomic.Uint64

type store interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Factory creates unique records that satisfy the current production schema.
// Pass t.Name() as namespace so independently running test packages do not
// collide on unique emails, registry IDs, INNs or agreement numbers.
type Factory struct {
	db        store
	namespace string
	now       time.Time
	sequence  *atomic.Uint64
}

func New(db *sql.DB, namespace string) *Factory {
	now := time.Now().UTC()
	if db == nil {
		return newFactory(nil, uniqueNamespace(namespace, now), now, &atomic.Uint64{})
	}
	return newFactory(db, uniqueNamespace(namespace, now), now, &atomic.Uint64{})
}

// NewTx creates fixtures inside a caller-owned transaction. This is the
// preferred form when a test wants to defer tx.Rollback for automatic cleanup.
func NewTx(tx *sql.Tx, namespace string) *Factory {
	now := time.Now().UTC()
	if tx == nil {
		return newFactory(nil, uniqueNamespace(namespace, now), now, &atomic.Uint64{})
	}
	return newFactory(tx, uniqueNamespace(namespace, now), now, &atomic.Uint64{})
}

func uniqueNamespace(namespace string, now time.Time) string {
	return fmt.Sprintf("%s-%x-%d", namespace, now.UnixNano(), factorySequence.Add(1))
}

func newFactory(db store, namespace string, now time.Time, sequence *atomic.Uint64) *Factory {
	namespace = strings.Trim(unsafeNamespace.ReplaceAllString(strings.ToLower(namespace), "-"), "-")
	if namespace == "" {
		namespace = "fixture"
	}
	return &Factory{db: db, namespace: namespace, now: now, sequence: sequence}
}

func (f *Factory) next(label string) (uint64, string, error) {
	if f == nil || f.db == nil {
		return 0, "", fmt.Errorf("test fixture database is required")
	}
	n := f.sequence.Add(1)
	return n, fmt.Sprintf("%s-%s-%d", f.namespace, label, n), nil
}

func (f *Factory) transaction(ctx context.Context, fn func(*Factory) error) error {
	if _, ok := f.db.(*sql.Tx); ok {
		return fn(f)
	}
	db, ok := f.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("test fixture store cannot start a transaction")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(newFactory(tx, f.namespace, f.now, f.sequence)); err != nil {
		return err
	}
	return tx.Commit()
}

type UserParams struct {
	Email       string
	Password    string
	FullName    string
	Role        models.Role
	EntityType  models.EntityType
	PartnerID   string
	ITCompanyID string
}

type User struct {
	ID          string
	Email       string
	Password    string
	FullName    string
	Role        models.Role
	EntityType  models.EntityType
	PartnerID   string
	ITCompanyID string
}

func (f *Factory) CreateUser(ctx context.Context, params UserParams) (User, error) {
	_, unique, err := f.next("user")
	if err != nil {
		return User{}, err
	}
	if params.Email == "" {
		params.Email = unique + "@fixture.invalid"
	}
	if params.Password == "" {
		params.Password = DefaultPassword
	}
	if params.FullName == "" {
		params.FullName = "Тестовый пользователь " + unique
	}
	if params.Role == "" {
		params.Role = models.RoleOrgAdmin
	}
	if params.EntityType == "" {
		params.EntityType = models.EntityOrganization
	}
	hash, err := hashFixturePassword(params.Password)
	if err != nil {
		return User{}, fmt.Errorf("hash fixture password: %w", err)
	}
	var id string
	err = f.db.QueryRowContext(ctx, `INSERT INTO users(
		email,password_hash,full_name,role,entity_type,partner_id,it_company_id
	) VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,NULLIF($7,'')::uuid) RETURNING id::text`,
		strings.ToLower(strings.TrimSpace(params.Email)), hash, params.FullName, params.Role, params.EntityType,
		params.PartnerID, params.ITCompanyID).Scan(&id)
	if err != nil {
		return User{}, fmt.Errorf("create user fixture: %w", err)
	}
	return User{ID: id, Email: strings.ToLower(strings.TrimSpace(params.Email)), Password: params.Password,
		FullName: params.FullName, Role: params.Role, EntityType: params.EntityType,
		PartnerID: params.PartnerID, ITCompanyID: params.ITCompanyID}, nil
}

type ITCompanyParams struct {
	Name      string
	INN       string
	OGRN      string
	CreatedBy string
}

type ITCompany struct {
	ID   string
	Name string
	INN  string
	OGRN string
}

func (f *Factory) CreateITCompany(ctx context.Context, params ITCompanyParams) (ITCompany, error) {
	n, unique, err := f.next("company")
	if err != nil {
		return ITCompany{}, err
	}
	if params.CreatedBy == "" {
		return ITCompany{}, fmt.Errorf("company fixture requires CreatedBy")
	}
	if params.Name == "" {
		params.Name = "Тестовая ИТ-организация " + unique
	}
	if params.INN == "" {
		params.INN = inn10(unique)
	}
	if params.OGRN == "" {
		params.OGRN = ogrn13(unique)
	}
	var id string
	err = f.db.QueryRowContext(ctx, `INSERT INTO accredited_it_companies(
		name,inn,ogrn,accreditation_status,registry_record_id,registry_updated_at,source_url,created_by
	) VALUES($1,$2,$3,'active',$4,$5,$6,$7::uuid) RETURNING id::text`, params.Name, params.INN,
		params.OGRN, unique, f.now, fmt.Sprintf("https://fixture.invalid/it-companies/%d", n), params.CreatedBy).Scan(&id)
	if err != nil {
		return ITCompany{}, fmt.Errorf("create IT company fixture: %w", err)
	}
	return ITCompany{ID: id, Name: params.Name, INN: params.INN, OGRN: params.OGRN}, nil
}

func (f *Factory) AssignUserToCompany(ctx context.Context, userID, companyID string) error {
	result, err := f.db.ExecContext(ctx, `UPDATE users SET it_company_id=$1::uuid,updated_at=now() WHERE id=$2::uuid`, companyID, userID)
	if err != nil {
		return fmt.Errorf("assign fixture user to company: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return fmt.Errorf("assign fixture user to company: user not found")
	}
	return nil
}

type EducationParams struct {
	Name         string
	Kind         string
	Region       string
	INN          string
	OGRN         string
	ProgramCodes []string
}

type EducationInstitution struct {
	ID           string
	Name         string
	Kind         string
	Region       string
	INN          string
	OGRN         string
	ProgramCodes []string
}

func (f *Factory) CreateUniversity(ctx context.Context, params EducationParams) (EducationInstitution, error) {
	params.Kind = "vuz"
	if len(params.ProgramCodes) == 0 {
		params.ProgramCodes = []string{"09.03.01"}
	}
	return f.CreateEducationInstitution(ctx, params)
}

func (f *Factory) CreateCollege(ctx context.Context, params EducationParams) (EducationInstitution, error) {
	params.Kind = "kolledj"
	if len(params.ProgramCodes) == 0 {
		params.ProgramCodes = []string{"09.02.01"}
	}
	return f.CreateEducationInstitution(ctx, params)
}

func (f *Factory) CreateSchool(ctx context.Context, params EducationParams) (EducationInstitution, error) {
	params.Kind = "school"
	if params.ProgramCodes == nil {
		params.ProgramCodes = []string{}
	}
	return f.CreateEducationInstitution(ctx, params)
}

func (f *Factory) CreateEducationInstitution(ctx context.Context, params EducationParams) (EducationInstitution, error) {
	n, unique, err := f.next("education")
	if err != nil {
		return EducationInstitution{}, err
	}
	if params.Kind != "vuz" && params.Kind != "kolledj" && params.Kind != "school" {
		return EducationInstitution{}, fmt.Errorf("education fixture kind must be vuz, kolledj or school")
	}
	if params.ProgramCodes == nil {
		params.ProgramCodes = []string{}
	}
	if params.Name == "" {
		labels := map[string]string{"vuz": "вуз", "kolledj": "СПО", "school": "школа"}
		params.Name = "Тестовый " + labels[params.Kind] + " " + unique
	}
	if params.Region == "" {
		params.Region = "Тестовый регион"
	}
	if params.INN == "" {
		params.INN = inn10(unique)
	}
	if params.OGRN == "" {
		params.OGRN = ogrn13(unique)
	}
	var id string
	err = f.db.QueryRowContext(ctx, `INSERT INTO education_directory(
		name,partner_kind,region,source,inn,ogrn,license_number,license_status,institution_status,
		registry_record_id,source_url,registry_updated_at,verified_at,verification_status,
		program_codes,programs_source_url,programs_checked_at,listed_in_mincifry_order_27
	) VALUES($1,$2,$3,'shared test fixture',$4,$5,$6,'active','active',$7,$8,$9,now(),'verified',
		$10,$11,now(),TRUE) RETURNING id::text`, params.Name, params.Kind, params.Region, params.INN,
		params.OGRN, "FIXTURE-LICENSE-"+unique, unique, fmt.Sprintf("https://fixture.invalid/education/%d", n),
		f.now, pq.Array(params.ProgramCodes), fmt.Sprintf("https://fixture.invalid/education/%d/programs", n)).Scan(&id)
	if err != nil {
		return EducationInstitution{}, fmt.Errorf("create education fixture: %w", err)
	}
	return EducationInstitution{ID: id, Name: params.Name, Kind: params.Kind, Region: params.Region,
		INN: params.INN, OGRN: params.OGRN, ProgramCodes: append([]string(nil), params.ProgramCodes...)}, nil
}

type Partner struct {
	ID          string
	Name        string
	Kind        string
	DirectoryID string
	CompanyID   string
}

func (f *Factory) CreatePartner(ctx context.Context, company ITCompany, institution EducationInstitution) (Partner, error) {
	var partner Partner
	err := f.db.QueryRowContext(ctx, `INSERT INTO partners(name,partner_kind,directory_id,it_company_id)
		VALUES($1,$2,$3::uuid,$4::uuid) RETURNING id::text,name,partner_kind,directory_id::text,it_company_id::text`,
		institution.Name, institution.Kind, institution.ID, company.ID).
		Scan(&partner.ID, &partner.Name, &partner.Kind, &partner.DirectoryID, &partner.CompanyID)
	if err != nil {
		return Partner{}, fmt.Errorf("create partner fixture: %w", err)
	}
	return partner, nil
}

type RegionalAuthorityParams struct {
	Name      string
	Region    string
	INN       string
	OGRN      string
	CreatedBy string
}

type RegionalAuthority struct {
	ID     string
	Name   string
	Region string
	INN    string
	OGRN   string
}

func (f *Factory) CreateRegionalAuthority(ctx context.Context, params RegionalAuthorityParams) (RegionalAuthority, error) {
	n, unique, err := f.next("roiv")
	if err != nil {
		return RegionalAuthority{}, err
	}
	if params.CreatedBy == "" {
		return RegionalAuthority{}, fmt.Errorf("regional authority fixture requires CreatedBy")
	}
	if params.Name == "" {
		params.Name = "Министерство образования " + unique
	}
	if params.Region == "" {
		params.Region = "Тестовый регион " + unique
	}
	if params.INN == "" {
		params.INN = inn10(unique)
	}
	if params.OGRN == "" {
		params.OGRN = ogrn13(unique)
	}
	var id string
	err = f.db.QueryRowContext(ctx, `INSERT INTO regional_authorities(
		name,region,inn,ogrn,status,source_url,created_by,updated_by
	) VALUES($1,$2,$3,$4,'active',$5,$6::uuid,$6::uuid) RETURNING id::text`, params.Name, params.Region,
		params.INN, params.OGRN, fmt.Sprintf("https://fixture.invalid/authorities/%d", n), params.CreatedBy).Scan(&id)
	if err != nil {
		return RegionalAuthority{}, fmt.Errorf("create regional authority fixture: %w", err)
	}
	return RegionalAuthority{ID: id, Name: params.Name, Region: params.Region, INN: params.INN, OGRN: params.OGRN}, nil
}

type AgreementParams struct {
	Kind                string
	Number              string
	CompanyID           string
	PartnerIDs          []string
	RegionalAuthorityID string
	RegionalAuthority   string
	ActivityCodes       []string
	CreatedBy           string
}

type Agreement struct {
	ID                  string
	Kind                string
	Number              string
	CompanyID           string
	PartnerIDs          []string
	RegionalAuthorityID string
	ActivityCodes       []string
}

func (f *Factory) CreateAgreement(ctx context.Context, params AgreementParams) (Agreement, error) {
	_, unique, err := f.next("agreement")
	if err != nil {
		return Agreement{}, err
	}
	if params.CompanyID == "" || params.CreatedBy == "" || len(params.PartnerIDs) == 0 {
		return Agreement{}, fmt.Errorf("agreement fixture requires CompanyID, CreatedBy and at least one partner")
	}
	if params.Kind == "" {
		params.Kind = "education_organization"
	}
	if params.Kind != "education_organization" && params.Kind != "roiv" {
		return Agreement{}, fmt.Errorf("agreement fixture kind must be education_organization or roiv")
	}
	if params.Kind == "roiv" && params.RegionalAuthorityID == "" {
		return Agreement{}, fmt.Errorf("ROIV agreement fixture requires RegionalAuthorityID")
	}
	if params.Number == "" {
		params.Number = "FIXTURE-" + strings.ToUpper(unique)
	}
	if params.RegionalAuthority == "" && params.Kind == "roiv" {
		params.RegionalAuthority = "Тестовый РОИВ"
	}
	if len(params.ActivityCodes) == 0 {
		if params.Kind == "roiv" {
			params.ActivityCodes = []string{"it_clubs"}
		} else {
			params.ActivityCodes = []string{"teachers", "ood_rpd"}
		}
	}
	record := Agreement{Kind: params.Kind, Number: params.Number, CompanyID: params.CompanyID,
		PartnerIDs: append([]string(nil), params.PartnerIDs...), RegionalAuthorityID: params.RegionalAuthorityID,
		ActivityCodes: append([]string(nil), params.ActivityCodes...)}
	err = f.transaction(ctx, func(txFactory *Factory) error {
		signedOn := time.Date(f.now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		validUntil := time.Date(f.now.Year()+2, 12, 31, 0, 0, 0, 0, time.UTC)
		if err := txFactory.db.QueryRowContext(ctx, `INSERT INTO agreements(
			agreement_kind,number,status,signed_on,valid_from,valid_until,regional_authority_id,roiv_name,
			it_company_id,signature_method,signed_by,signature_date,document_reference,created_by,updated_by
		) VALUES($1,$2,'active',$3,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,''),$7::uuid,
			'qualified_electronic','Тестовый подписант',$3,'shared test fixture',$8::uuid,$8::uuid)
			RETURNING id::text`, params.Kind, params.Number, signedOn, validUntil, params.RegionalAuthorityID,
			params.RegionalAuthority, params.CompanyID, params.CreatedBy).Scan(&record.ID); err != nil {
			return fmt.Errorf("create agreement fixture: %w", err)
		}
		for index, partnerID := range params.PartnerIDs {
			if _, err := txFactory.db.ExecContext(ctx, `INSERT INTO agreement_partners(agreement_id,partner_id,is_primary)
				VALUES($1::uuid,$2::uuid,$3)`, record.ID, partnerID, index == 0); err != nil {
				return fmt.Errorf("link agreement fixture partner: %w", err)
			}
		}
		for _, person := range []struct{ party, name string }{
			{"cyberprotect", "Ответственный ИТ-компании"}, {"counterparty", "Ответственный контрагента"},
		} {
			if _, err := txFactory.db.ExecContext(ctx, `INSERT INTO agreement_responsible_people(agreement_id,party,full_name)
				VALUES($1::uuid,$2,$3)`, record.ID, person.party, person.name); err != nil {
				return fmt.Errorf("create agreement responsible person fixture: %w", err)
			}
		}
		for _, code := range params.ActivityCodes {
			if _, err := txFactory.db.ExecContext(ctx, `INSERT INTO agreement_activity_requirements(agreement_id,category_code)
				VALUES($1::uuid,$2)`, record.ID, code); err != nil {
				return fmt.Errorf("create agreement activity fixture: %w", err)
			}
		}
		return nil
	})
	return record, err
}

// Scenario is the common connected graph used by integration tests. It has
// one IT-company tenant, all supported education kinds, an ROIV, valid active
// agreements, and users scoped to both sides of those agreements.
type Scenario struct {
	Admin               User
	CompanyAdmin        User
	UniversityUser      User
	CollegeUser         User
	SchoolUser          User
	Company             ITCompany
	University          EducationInstitution
	College             EducationInstitution
	School              EducationInstitution
	UniversityPartner   Partner
	CollegePartner      Partner
	SchoolPartner       Partner
	RegionalAuthority   RegionalAuthority
	UniversityAgreement Agreement
	CollegeAgreement    Agreement
	SchoolAgreement     Agreement
}

func (f *Factory) CreateScenario(ctx context.Context) (Scenario, error) {
	var scenario Scenario
	err := f.transaction(ctx, func(txFactory *Factory) error {
		var err error
		scenario.Admin, err = txFactory.CreateUser(ctx, UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
		if err != nil {
			return err
		}
		scenario.Company, err = txFactory.CreateITCompany(ctx, ITCompanyParams{CreatedBy: scenario.Admin.ID})
		if err != nil {
			return err
		}
		if err = txFactory.AssignUserToCompany(ctx, scenario.Admin.ID, scenario.Company.ID); err != nil {
			return err
		}
		scenario.University, err = txFactory.CreateUniversity(ctx, EducationParams{})
		if err != nil {
			return err
		}
		scenario.College, err = txFactory.CreateCollege(ctx, EducationParams{})
		if err != nil {
			return err
		}
		scenario.School, err = txFactory.CreateSchool(ctx, EducationParams{})
		if err != nil {
			return err
		}
		scenario.UniversityPartner, err = txFactory.CreatePartner(ctx, scenario.Company, scenario.University)
		if err != nil {
			return err
		}
		scenario.CollegePartner, err = txFactory.CreatePartner(ctx, scenario.Company, scenario.College)
		if err != nil {
			return err
		}
		scenario.SchoolPartner, err = txFactory.CreatePartner(ctx, scenario.Company, scenario.School)
		if err != nil {
			return err
		}
		scenario.RegionalAuthority, err = txFactory.CreateRegionalAuthority(ctx, RegionalAuthorityParams{CreatedBy: scenario.Admin.ID})
		if err != nil {
			return err
		}
		scenario.UniversityAgreement, err = txFactory.CreateAgreement(ctx, AgreementParams{CompanyID: scenario.Company.ID,
			PartnerIDs: []string{scenario.UniversityPartner.ID}, CreatedBy: scenario.Admin.ID})
		if err != nil {
			return err
		}
		scenario.CollegeAgreement, err = txFactory.CreateAgreement(ctx, AgreementParams{CompanyID: scenario.Company.ID,
			PartnerIDs: []string{scenario.CollegePartner.ID}, CreatedBy: scenario.Admin.ID})
		if err != nil {
			return err
		}
		scenario.SchoolAgreement, err = txFactory.CreateAgreement(ctx, AgreementParams{Kind: "roiv", CompanyID: scenario.Company.ID,
			PartnerIDs: []string{scenario.SchoolPartner.ID}, RegionalAuthorityID: scenario.RegionalAuthority.ID,
			RegionalAuthority: scenario.RegionalAuthority.Name, CreatedBy: scenario.Admin.ID})
		if err != nil {
			return err
		}
		scenario.CompanyAdmin, err = txFactory.CreateUser(ctx, UserParams{Role: models.RoleOrgAdmin,
			EntityType: models.EntityOrganization, ITCompanyID: scenario.Company.ID})
		if err != nil {
			return err
		}
		scenario.UniversityUser, err = txFactory.CreateUser(ctx, UserParams{Role: models.RoleOrgAdmin,
			EntityType: models.EntityEduInst, PartnerID: scenario.UniversityPartner.ID})
		if err != nil {
			return err
		}
		scenario.CollegeUser, err = txFactory.CreateUser(ctx, UserParams{Role: models.RoleOrgAdmin,
			EntityType: models.EntityEduInst, PartnerID: scenario.CollegePartner.ID})
		if err != nil {
			return err
		}
		scenario.SchoolUser, err = txFactory.CreateUser(ctx, UserParams{Role: models.RoleOrgAdmin,
			EntityType: models.EntityEduInst, PartnerID: scenario.SchoolPartner.ID})
		return err
	})
	return scenario, err
}

func decimalDigits(seed string, count int) string {
	digest := sha256.Sum256([]byte(seed))
	var builder strings.Builder
	for index := 0; index < count; index++ {
		builder.WriteByte('0' + digest[index%len(digest)]%10)
	}
	return builder.String()
}

// Keep testfixtures independent of internal/auth so auth package tests can use
// the shared factories without an import cycle. The wire format and work
// factor intentionally match the production password contract.
func hashFixturePassword(password string) (string, error) {
	if password == "" || len(password) > 512 {
		return "", fmt.Errorf("invalid fixture password length")
	}
	const iterations = 600_000
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$%d$%s$%s", iterations,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func inn10(seed string) string {
	base := decimalDigits(seed+"-inn", 9)
	weights := []int{2, 4, 10, 3, 5, 9, 4, 6, 8}
	sum := 0
	for index, weight := range weights {
		sum += int(base[index]-'0') * weight
	}
	return base + string(rune('0'+(sum%11)%10))
}

func ogrn13(seed string) string {
	base := "1" + decimalDigits(seed+"-ogrn", 11)
	var value uint64
	for _, digit := range base {
		value = value*10 + uint64(digit-'0')
	}
	return base + string(rune('0'+value%11%10))
}
