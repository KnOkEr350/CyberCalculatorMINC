package handlers

import (
	"cybercalc/internal/middleware"
	"fmt"
	"net/http"
	"strconv"
)

const listPageSize = 500

func pageClause(w http.ResponseWriter, r *http.Request) (string, bool) {
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		var err error
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 || offset > 1000000 {
			middleware.WriteError(w, 400, "некорректная страница")
			return "", false
		}
	}
	return fmt.Sprintf(" LIMIT %d OFFSET %d", listPageSize+1, offset), true
}

func writePage[T any](w http.ResponseWriter, r *http.Request, rows []T) {
	if len(rows) > listPageSize {
		rows = rows[:listPageSize]
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		w.Header().Set("X-Next-Offset", strconv.Itoa(offset+listPageSize))
	}
	middleware.WriteJSON(w, 200, rows)
}
