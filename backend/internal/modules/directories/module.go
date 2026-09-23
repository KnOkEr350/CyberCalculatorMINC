// Package directories owns organization and reference-data route registration.
package directories

import (
	"database/sql"
	"net/http"

	"cybercalc/internal/handlers"
	"cybercalc/internal/middleware"
)

type Module struct {
	db                  *sql.DB
	partners            *handlers.PartnerHandlers
	itCompanies         *handlers.ITCompanyHandlers
	agreements          *handlers.AgreementHandlers
	legalEntityGroups   *handlers.LegalEntityGroupHandlers
	regionalAuthorities *handlers.RegionalAuthorityHandlers
	entryReferences     *handlers.EntryHandlers
	partnerStructure    *handlers.PartnerStructureHandlers
	referenceCatalogs   *handlers.ReferenceCatalogHandlers
	curatorAssignments  *handlers.CuratorAssignmentHandlers
}

func New(db *sql.DB) *Module {
	return &Module{
		db: db, partners: &handlers.PartnerHandlers{DB: db}, itCompanies: &handlers.ITCompanyHandlers{DB: db},
		agreements: &handlers.AgreementHandlers{DB: db}, legalEntityGroups: &handlers.LegalEntityGroupHandlers{DB: db},
		regionalAuthorities: &handlers.RegionalAuthorityHandlers{DB: db}, entryReferences: &handlers.EntryHandlers{DB: db},
		partnerStructure:   &handlers.PartnerStructureHandlers{DB: db},
		referenceCatalogs:  &handlers.ReferenceCatalogHandlers{DB: db},
		curatorAssignments: &handlers.CuratorAssignmentHandlers{DB: db},
	}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/partners", middleware.RequireAuth(m.db, m.partners.List))
	mux.HandleFunc("POST /api/partners", middleware.RequireAuth(m.db, m.partners.Create))
	mux.HandleFunc("GET /api/partners/{partner_id}/org-units", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.partnerStructure.ListOrgUnits(w, r, u, r.PathValue("partner_id"))
	}))
	mux.HandleFunc("POST /api/partners/{partner_id}/org-units", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.partnerStructure.CreateOrgUnit(w, r, u, r.PathValue("partner_id"))
	}))
	mux.HandleFunc("PUT /api/org-units/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.partnerStructure.UpdateOrgUnit(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("DELETE /api/org-units/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.partnerStructure.DeleteOrgUnit(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/partners/{partner_id}/academic-groups", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.partnerStructure.ListAcademicGroups(w, r, u, r.PathValue("partner_id"))
	}))
	mux.HandleFunc("POST /api/partners/{partner_id}/academic-groups", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.partnerStructure.CreateAcademicGroup(w, r, u, r.PathValue("partner_id"))
	}))
	mux.HandleFunc("PUT /api/academic-groups/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.partnerStructure.UpdateAcademicGroup(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("DELETE /api/academic-groups/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.partnerStructure.DeleteAcademicGroup(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/partners/{partner_id}/specialty-codes", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.partnerStructure.SpecialtyCodes(w, r, u, r.PathValue("partner_id"))
	}))
	mux.HandleFunc("GET /api/it-companies", middleware.RequireAuth(m.db, m.itCompanies.List))
	mux.HandleFunc("GET /api/it-companies/registry-search", middleware.RequireAuth(m.db, m.itCompanies.RegistrySearch))
	mux.HandleFunc("POST /api/it-companies", middleware.RequireAuth(m.db, m.itCompanies.Create))
	mux.HandleFunc("GET /api/it-companies/template", middleware.RequireAuth(m.db, m.itCompanies.Template))
	mux.HandleFunc("POST /api/it-companies/import", middleware.RequireAuth(m.db, m.itCompanies.Import))
	mux.HandleFunc("GET /api/directory", middleware.RequireAuth(m.db, m.partners.Directory))
	mux.HandleFunc("GET /api/directory/stats", middleware.RequireAuth(m.db, m.partners.DirectoryStats))
	mux.HandleFunc("POST /api/directory", middleware.RequireAuth(m.db, m.partners.CreateDirectory))
	mux.HandleFunc("GET /api/directory/proposals", middleware.RequireAuth(m.db, m.partners.DirectoryProposals))
	mux.HandleFunc("POST /api/directory/{id}/decision", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.partners.DecideDirectoryProposal(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("PUT /api/directory/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.partners.UpdateDirectory(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/agreements", middleware.RequireAuth(m.db, m.agreements.List))
	mux.HandleFunc("POST /api/agreements", middleware.RequireAuth(m.db, m.agreements.Create))
	mux.HandleFunc("PUT /api/agreements/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.agreements.Update(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/legal-entity-groups", middleware.RequireAuth(m.db, m.legalEntityGroups.List))
	mux.HandleFunc("POST /api/legal-entity-groups", middleware.RequireAuth(m.db, m.legalEntityGroups.Create))
	mux.HandleFunc("PUT /api/legal-entity-groups/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.legalEntityGroups.Update(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/regional-authorities", middleware.RequireAuth(m.db, m.regionalAuthorities.List))
	mux.HandleFunc("POST /api/regional-authorities", middleware.RequireAuth(m.db, m.regionalAuthorities.Create))
	mux.HandleFunc("PUT /api/regional-authorities/{id}", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.regionalAuthorities.Update(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/mentors", middleware.RequireAuth(m.db, m.entryReferences.Mentors))
	mux.HandleFunc("POST /api/mentors", middleware.RequireAuth(m.db, m.entryReferences.CreateMentor))
	mux.HandleFunc("GET /api/admin/directory-template", middleware.RequireAuth(m.db, m.partners.DirectoryTemplate))
	mux.HandleFunc("POST /api/admin/directory-import", middleware.RequireAuth(m.db, m.partners.ImportDirectory))
	mux.HandleFunc("GET /api/organizations", middleware.RequireAuth(m.db, m.referenceCatalogs.Organizations))
	mux.HandleFunc("GET /api/specialties", middleware.RequireAuth(m.db, m.referenceCatalogs.Specialties))
	mux.HandleFunc("POST /api/specialty-catalogs/import", middleware.RequireAuth(m.db, m.referenceCatalogs.ImportSpecialties))
	mux.HandleFunc("GET /api/curator-assignments", middleware.RequireAuth(m.db, m.curatorAssignments.List))
	mux.HandleFunc("POST /api/curator-assignments", middleware.RequireAuth(m.db, m.curatorAssignments.Create))
	mux.HandleFunc("POST /api/curator-assignments/{id}/end", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.curatorAssignments.End(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("POST /api/curator-assignments/{id}/revoke", middleware.RequireAuth(m.db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		m.curatorAssignments.Revoke(w, r, u, r.PathValue("id"))
	}))
}
