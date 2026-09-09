package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
)

func decodeJSON(r *http.Request, v interface{}) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return fmt.Errorf("ожидается application/json")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("тело запроса должно содержать один JSON-объект")
		}
		return err
	}
	return nil
}
