package handlers

import (
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"cybercalc/internal/models"
)

func validateAndNormalizeNewUser(req *createUserRequest) error {
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" || utf8.RuneCountInString(req.Email) > 254 || strings.ContainsAny(req.Email, "\r\n\t ") {
		return fmt.Errorf("укажите корректный email длиной не более 254 символов")
	}
	parsed, err := mail.ParseAddress(req.Email)
	if err != nil || !strings.EqualFold(parsed.Address, req.Email) {
		return fmt.Errorf("укажите корректный email")
	}

	req.FullName = strings.Join(strings.Fields(req.FullName), " ")
	nameLength := utf8.RuneCountInString(req.FullName)
	if nameLength < 2 || nameLength > 200 {
		return fmt.Errorf("ФИО должно содержать от 2 до 200 символов")
	}
	hasLetter := false
	for _, r := range req.FullName {
		if unicode.IsControl(r) {
			return fmt.Errorf("ФИО содержит недопустимые символы")
		}
		if unicode.IsLetter(r) {
			hasLetter = true
		}
	}
	if !hasLetter {
		return fmt.Errorf("ФИО должно содержать буквы")
	}

	passwordLength := utf8.RuneCountInString(req.Password)
	if passwordLength < 10 || passwordLength > 128 {
		return fmt.Errorf("пароль должен содержать от 10 до 128 символов")
	}
	var lower, upper, digit, special bool
	for _, r := range req.Password {
		switch {
		case unicode.IsSpace(r), unicode.IsControl(r):
			return fmt.Errorf("пароль не должен содержать пробелы или управляющие символы")
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			special = true
		}
	}
	if !lower || !upper || !digit || !special {
		return fmt.Errorf("пароль должен содержать строчную и заглавную буквы, цифру и специальный символ")
	}

	if req.Role != string(models.RoleAdmin) && req.Role != string(models.RoleUser) {
		return fmt.Errorf("роль должна быть admin или user")
	}
	if req.EntityType != "" && req.EntityType != string(models.EntityOrganization) && req.EntityType != string(models.EntityEduInst) {
		return fmt.Errorf("тип пользователя должен быть organization или edu_institution")
	}
	if req.PartnerID != nil {
		trimmed := strings.TrimSpace(*req.PartnerID)
		if trimmed == "" {
			req.PartnerID = nil
		} else {
			req.PartnerID = &trimmed
			if req.EntityType != string(models.EntityEduInst) {
				return fmt.Errorf("партнёра можно назначить только пользователю образовательной организации")
			}
		}
	}
	if req.EntityType == string(models.EntityEduInst) && req.PartnerID == nil {
		return fmt.Errorf("для образовательной организации необходимо выбрать партнёра")
	}
	return nil
}

func digits(value string, lengths ...int) ([]int, bool) {
	validLength := false
	for _, length := range lengths {
		if len(value) == length {
			validLength = true
		}
	}
	if !validLength {
		return nil, false
	}
	out := make([]int, len(value))
	for i, r := range value {
		if r < '0' || r > '9' {
			return nil, false
		}
		out[i] = int(r - '0')
	}
	return out, true
}

func weightedCheck(values, weights []int) int {
	total := 0
	for i, weight := range weights {
		total += values[i] * weight
	}
	return total % 11 % 10
}

func validINN(value string) bool {
	numbers, ok := digits(value, 10, 12)
	if !ok {
		return false
	}
	if len(numbers) == 10 {
		return weightedCheck(numbers, []int{2, 4, 10, 3, 5, 9, 4, 6, 8}) == numbers[9]
	}
	return weightedCheck(numbers, []int{7, 2, 4, 10, 3, 5, 9, 4, 6, 8}) == numbers[10] &&
		weightedCheck(numbers, []int{3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8}) == numbers[11]
}

func validOGRN(value string) bool {
	numbers, ok := digits(value, 13, 15)
	if !ok {
		return false
	}
	prefix, err := strconv.ParseUint(value[:len(value)-1], 10, 64)
	if err != nil {
		return false
	}
	modulus := uint64(11)
	if len(numbers) == 15 {
		modulus = 13
	}
	return int(prefix%modulus%10) == numbers[len(numbers)-1]
}

func officialRegistryURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	// The federal register is the preferred source. Regional licensing
	// authorities may publish official extracts on government domains.
	return host == "obrnadzor.gov.ru" || strings.HasSuffix(host, ".obrnadzor.gov.ru") || strings.HasSuffix(host, ".gov.ru")
}
